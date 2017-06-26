package steps_test

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/executor/depot/log_streamer/fake_log_streamer"
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/steps/fakes"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("LongRunningMonitorStep", func() {
	var (
		readinessCheckFunc, livenessCheckFunc func(log_streamer.LogStreamer) steps.Step
		readinessCheck, livenessCheck         *fakes.FakeStep
		hasBecomeHealthy                      <-chan struct{}
		clock                                 *fakeclock.FakeClock
		fakeStreamer                          *fake_log_streamer.FakeLogStreamer

		// monitorErr string

		startTimeout      time.Duration
		healthyInterval   time.Duration
		unhealthyInterval time.Duration

		step   steps.Step
		logger *lagertest.TestLogger
	)

	BeforeEach(func() {
		startTimeout = 1 * time.Second
		healthyInterval = 1 * time.Second
		unhealthyInterval = 500 * time.Millisecond

		readinessCheck = new(fakes.FakeStep)
		livenessCheck = new(fakes.FakeStep)
		readinessCheckFunc = func(log_streamer.LogStreamer) steps.Step { return readinessCheck }
		livenessCheckFunc = func(log_streamer.LogStreamer) steps.Step { return livenessCheck }

		clock = fakeclock.NewFakeClock(time.Now())

		fakeStreamer = newFakeStreamer()

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		hasBecomeHealthyChannel := make(chan struct{}, 1000)
		hasBecomeHealthy = hasBecomeHealthyChannel

		fakeStreamer.WithSourceReturns(fakeStreamer)

		step = steps.NewLongRunningMonitor(
			readinessCheckFunc,
			livenessCheckFunc,
			hasBecomeHealthyChannel,
			logger,
			clock,
			fakeStreamer,
			startTimeout,
		)
	})

	Describe("Perform", func() {
		var (
			readinessResults chan error
			livenessResults  chan error

			performErr     chan error
			donePerforming *sync.WaitGroup
		)

		BeforeEach(func() {
			readinessResults = make(chan error, 10)
			livenessResults = make(chan error, 10)

			readinessCheck.PerformStub = func() error {
				return <-readinessResults
			}
			livenessCheck.PerformStub = func() error {
				return <-livenessResults
			}
		})

		JustBeforeEach(func() {
			performErr = make(chan error, 1)
			donePerforming = new(sync.WaitGroup)

			donePerforming.Add(1)
			go func() {
				defer donePerforming.Done()
				performErr <- step.Perform()
			}()
		})

		AfterEach(func() {
			close(readinessResults)
			close(livenessResults)
			donePerforming.Wait()
		})

		It("emits a message to the applications log stream", func() {
			Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
				gbytes.Say("Starting health monitoring of container\n"),
			)
		})

		Context("when the readiness check fails", func() {
			BeforeEach(func() {
				readinessResults <- errors.New("booom!")
			})

			It("completes with failure", func() {
				var expectedError interface{}
				Eventually(performErr).Should(Receive(&expectedError))
				err, ok := expectedError.(*steps.EmittableError)
				Expect(ok).To(BeTrue())
				Expect(err.WrappedError()).To(MatchError(ContainSubstring("booom!")))
			})
		})

		Context("when the readiness check passes", func() {
			BeforeEach(func() {
				readinessResults <- nil
			})

			It("emits a healthy event", func() {
				Eventually(hasBecomeHealthy).Should(Receive())
			})

			It("emits a log message for the success", func() {
				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
					gbytes.Say("Container became healthy\n"),
				)
			})

			It("logs the step", func() {
				Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
					"test.monitor-step.transitioned-to-healthy",
				}))
			})

			Context("and the liveness check fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					livenessResults <- disaster
					livenessCheckFunc = func(streamer log_streamer.LogStreamer) steps.Step {
						streamer.Stdout().Write([]byte("healthcheck failed"))
						return livenessCheck
					}
				})

				JustBeforeEach(func() {
					Eventually(hasBecomeHealthy).Should(Receive())
				})

				It("emits nothing", func() {
					Consistently(hasBecomeHealthy).ShouldNot(Receive())
				})

				It("logs the step", func() {
					Eventually(func() []string { return logger.TestSink.LogMessages() }).Should(ConsistOf([]string{
						"test.monitor-step.transitioned-to-healthy",
						"test.monitor-step.transitioned-to-unhealthy",
					}))
				})

				It("emits a log message for the failure", func() {
					Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
						gbytes.Say("Container became unhealthy\n"),
					)
				})

				It("emits the healthcheck process response for the failure", func() {
					Eventually(fakeStreamer.Stderr().(*gbytes.Buffer)).Should(
						gbytes.Say(fmt.Sprintf("healthcheck failed\n")),
					)
				})

				It("completes with failure", func() {
					var expectedError interface{}
					Eventually(performErr).Should(Receive(&expectedError))
					err, ok := expectedError.(*steps.EmittableError)
					Expect(ok).To(BeTrue())
					Expect(err.WrappedError()).To(Equal(disaster))
				})
			})
		})

		Context("and the start timeout is exceeded", func() {
			JustBeforeEach(func() {
				clock.WaitForWatcherAndIncrement(startTimeout)
			})

			It("cancels the running step", func() {
				Eventually(readinessCheck.CancelCallCount).Should(Equal(1))
			})

			Context("when the running step exits", func() {
				BeforeEach(func() {
					readinessCheckFunc = func(streamer log_streamer.LogStreamer) steps.Step {
						streamer.Stdout().Write([]byte("healthcheck failed"))
						return readinessCheck
					}
				})

				JustBeforeEach(func() {
					Eventually(readinessCheck.CancelCallCount).Should(Equal(1))
					readinessResults <- errors.New("interrupted")
				})

				It("completes with failure", func() {
					var expectedError interface{}
					Eventually(performErr).Should(Receive(&expectedError))
					err, ok := expectedError.(*steps.EmittableError)
					Expect(ok).To(BeTrue())
					Expect(err.WrappedError()).To(MatchError(ContainSubstring("interrupted")))
				})

				It("logs the step", func() {
					Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
						"test.monitor-step.timed-out-before-healthy",
					}))
				})

				It("emits the last healthcheck process response to the log stream", func() {
					Eventually(fakeStreamer.Stderr().(*gbytes.Buffer)).Should(
						gbytes.Say("healthcheck failed\n"),
					)
				})

				It("emits a log message explaining the timeout", func() {
					Eventually(fakeStreamer.Stderr().(*gbytes.Buffer)).Should(gbytes.Say(
						fmt.Sprintf("Timed out after %s: health check never passed.\n", startTimeout),
					))
				})

			})

			Context("when monitor step is composed of multiple checks", func() {
				BeforeEach(func() {
					bufferChan := make(chan io.Writer, 2)

					fakeStep1 := new(fakes.FakeStep)
					fakeStep2 := new(fakes.FakeStep)
					fakeStep1.PerformStub = func() error {
						writer := <-bufferChan
						writer.Write([]byte("another thing"))
						return errors.New("boooom!")
					}
					fakeStep2.PerformStub = func() error {
						writer := <-bufferChan
						writer.Write([]byte("something"))
						return nil
					}

					readinessCheckFunc = func(logstreamer log_streamer.LogStreamer) steps.Step {
						bufferChan <- logstreamer.Stdout()
						bufferChan <- logstreamer.Stdout()
						return steps.NewParallel([]steps.Step{fakeStep1, fakeStep2})
					}
				})

				// Generates a data race as a failure mode
				It("correctly serializes output and does not race", func() {
					Eventually(fakeStreamer.Stderr().(*gbytes.Buffer)).Should(gbytes.Say("something"))
				})
			})
		})
	})

	Describe("Cancel", func() {
		Context("while doing readiness check", func() {
			var performing chan struct{}

			BeforeEach(func() {
				performing = make(chan struct{})
				cancelled := make(chan struct{})

				readinessCheck.PerformStub = func() error {
					close(performing)

					select {
					case <-cancelled:
						return steps.ErrCancelled
					}
				}

				readinessCheck.CancelStub = func() {
					close(cancelled)
				}
			})

			It("cancels the in-flight check", func() {
				performResult := make(chan error)

				go func() { performResult <- step.Perform() }()

				Eventually(performing).Should(BeClosed())

				step.Cancel()

				Eventually(performResult).Should(Receive(Equal(steps.ErrCancelled)))
			})
		})

		Context("when readiness check passes", func() {
			BeforeEach(func() {
				readinessCheck.PerformReturns(nil)
			})

			Context("and while doing liveness check", func() {
				var performing chan struct{}

				BeforeEach(func() {
					performing = make(chan struct{})
					cancelled := make(chan struct{})

					livenessCheck.PerformStub = func() error {
						close(performing)

						select {
						case <-cancelled:
							return steps.ErrCancelled
						}
					}

					livenessCheck.CancelStub = func() {
						close(cancelled)
					}
				})

				It("cancels the in-flight check", func() {
					performResult := make(chan error)

					go func() { performResult <- step.Perform() }()

					Eventually(performing).Should(BeClosed())

					step.Cancel()

					Eventually(performResult).Should(Receive(Equal(steps.ErrCancelled)))
				})
			})
		})
	})
})
