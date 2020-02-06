package steps_test

import (
	"errors"
	"fmt"
	"os"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor/depot/log_streamer/fake_log_streamer"
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"
)

var _ = Describe("NewHealthCheckStep", func() {
	var (
		readinessCheck, livenessCheck *fake_runner.TestRunner
		clock                         *fakeclock.FakeClock
		fakeStreamer                  *fake_log_streamer.FakeLogStreamer
		fakeHealthCheckStreamer       *fake_log_streamer.FakeLogStreamer

		startTimeout time.Duration

		step    ifrit.Runner
		process ifrit.Process
		logger  *lagertest.TestLogger
	)

	BeforeEach(func() {
		startTimeout = 1 * time.Second

		readinessCheck = fake_runner.NewTestRunner()
		livenessCheck = fake_runner.NewTestRunner()

		clock = fakeclock.NewFakeClock(time.Now())

		fakeHealthCheckStreamer = newFakeStreamer()
		fakeStreamer = newFakeStreamer()

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		fakeStreamer.WithSourceReturns(fakeStreamer)

		step = steps.NewHealthCheckStep(
			readinessCheck,
			livenessCheck,
			logger,
			clock,
			fakeStreamer,
			fakeHealthCheckStreamer,
			startTimeout,
		)

		process = ifrit.Background(step)
	})

	AfterEach(func() {
		Eventually(readinessCheck.RunCallCount).Should(Equal(1))
		readinessCheck.EnsureExit()
		if livenessCheck != nil {
			Eventually(livenessCheck.RunCallCount).Should(Equal(1))
			livenessCheck.TriggerExit(errors.New("booom!")) // liveness check must exit with non-nil error
		}
	})

	Describe("Run", func() {
		It("emits a message to the applications log stream", func() {
			Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
				gbytes.Say("Starting health monitoring of container\n"),
			)
		})

		Context("when the readiness check fails", func() {
			JustBeforeEach(func() {
				readinessCheck.TriggerExit(errors.New("booom!"))
				livenessCheck = nil
			})

			It("completes with failure", func() {
				var err *steps.EmittableError
				Eventually(process.Wait()).Should(Receive(&err))
				Expect(err.WrappedError()).To(MatchError(ContainSubstring("booom!")))
			})

			It("logs the step", func() {
				Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
					"test.health-check-step.timed-out-before-healthy",
				}))
			})

			It("emits the last healthcheck process response to the log stream", func() {
				Eventually(fakeHealthCheckStreamer.Stderr().(*gbytes.Buffer)).Should(
					gbytes.Say("booom!\n"),
				)
			})

			It("emits a log message explaining the timeout", func() {
				Eventually(fakeStreamer.Stderr().(*gbytes.Buffer)).Should(gbytes.Say(
					"Failed after .*: readiness health check never passed.\n",
				))
			})
		})

		Context("when the readiness check passes", func() {
			JustBeforeEach(func() {
				readinessCheck.TriggerExit(nil)
			})

			It("becomes ready", func() {
				Eventually(process.Ready()).Should(BeClosed())
			})

			It("emits a log message for the success", func() {
				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
					gbytes.Say("Container became healthy\n"),
				)
			})

			It("logs the step", func() {
				Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
					"test.health-check-step.transitioned-to-healthy",
				}))
			})

			Context("and the liveness check fails", func() {
				disaster := errors.New("oh no!")

				JustBeforeEach(func() {
					livenessCheck.TriggerExit(disaster)
					livenessCheck = nil
				})

				It("logs the step", func() {
					Eventually(func() []string { return logger.TestSink.LogMessages() }).Should(ConsistOf([]string{
						"test.health-check-step.transitioned-to-healthy",
						"test.health-check-step.transitioned-to-unhealthy",
					}))
				})

				It("emits a log message for the failure", func() {
					Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
						gbytes.Say("Container became unhealthy\n"),
					)
				})

				It("emits the healthcheck process response for the failure", func() {
					Eventually(fakeHealthCheckStreamer.Stderr().(*gbytes.Buffer)).Should(
						gbytes.Say(fmt.Sprintf("oh no!\n")),
					)
				})

				It("completes with failure", func() {
					var err *steps.EmittableError
					Eventually(process.Wait()).Should(Receive(&err))
					Expect(err.WrappedError()).To(Equal(disaster))
				})
			})
		})
	})

	Describe("Signalling", func() {
		Context("while doing readiness check", func() {
			BeforeEach(func() {
				livenessCheck = nil
			})

			It("cancels the in-flight check", func() {
				Eventually(readinessCheck.RunCallCount).Should(Equal(1))

				process.Signal(os.Interrupt)
				Eventually(readinessCheck.WaitForCall()).Should(Receive(Equal(os.Interrupt)))
				readinessCheck.TriggerExit(nil)
				Eventually(process.Wait()).Should(Receive(Equal(new(steps.CancelledError))))
			})
		})

		Context("when readiness check passes", func() {
			Context("and while doing liveness check", func() {
				It("cancels the in-flight check", func() {
					readinessCheck.TriggerExit(nil)

					Eventually(livenessCheck.RunCallCount).Should(Equal(1))

					process.Signal(os.Interrupt)
					Eventually(livenessCheck.WaitForCall()).Should(Receive(Equal(os.Interrupt)))
					livenessCheck.TriggerExit(nil)
					livenessCheck = nil
					Eventually(process.Wait()).Should(Receive(Equal(new(steps.CancelledError))))
				})
			})
		})
	})
})
