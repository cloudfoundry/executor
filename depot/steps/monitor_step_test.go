package steps_test

import (
	"errors"
	"sync"
	"time"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MonitorStep", func() {
	var (
		check          *fakes.FakeStep
		receivedEvents <-chan HealthEvent
		timeProvider   *faketimeprovider.FakeTimeProvider

		startTimeout      time.Duration
		healthyInterval   time.Duration
		unhealthyInterval time.Duration

		step   Step
		logger *lagertest.TestLogger
	)

	BeforeEach(func() {
		startTimeout = 0
		healthyInterval = 1 * time.Second
		unhealthyInterval = 500 * time.Millisecond

		timeProvider = faketimeprovider.New(time.Now())
		check = new(fakes.FakeStep)
		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		events := make(chan HealthEvent, 1000)
		receivedEvents = events

		step = NewMonitor(
			check,
			events,
			logger,
			timeProvider,
			startTimeout,
			healthyInterval,
			unhealthyInterval,
		)
	})

	Describe("Perform", func() {
		var (
			checkResults chan<- error

			performErr     chan error
			donePerforming *sync.WaitGroup
		)

		expectCheckAfterInterval := func(d time.Duration) {
			previousCheckCount := check.PerformCallCount()

			timeProvider.Increment(d - 1*time.Microsecond)
			Consistently(check.PerformCallCount, 0.05).Should(Equal(previousCheckCount))

			timeProvider.Increment(d)
			Eventually(check.PerformCallCount).Should(Equal(previousCheckCount + 1))
		}

		BeforeEach(func() {
			results := make(chan error, 10)
			checkResults = results

			var currentResult error
			check.PerformStub = func() error {
				select {
				case currentResult = <-results:
				default:
				}

				return currentResult
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
			step.Cancel()
			donePerforming.Wait()
		})

		Context("when the check succeeds", func() {
			BeforeEach(func() {
				checkResults <- nil
			})

			Context("and the unhealthy interval passes", func() {
				JustBeforeEach(func() {
					expectCheckAfterInterval(unhealthyInterval)
				})

				It("emits a healthy event", func() {
					Eventually(receivedEvents).Should(Receive(Equal(Healthy)))
				})

				It("logs the step", func() {
					Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
						"test.monitor-step.transitioned-to-healthy",
					}))
				})

				Context("and the healthy interval passes", func() {
					JustBeforeEach(func() {
						Eventually(receivedEvents).Should(Receive(Equal(Healthy)))
						expectCheckAfterInterval(healthyInterval)
					})

					It("does not emit another healthy event", func() {
						Consistently(receivedEvents).ShouldNot(Receive())
					})
				})

				Context("and the check begins to fail", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						checkResults <- disaster
					})

					Context("and the healthy interval passes", func() {
						JustBeforeEach(func() {
							expectCheckAfterInterval(healthyInterval)
						})

						It("emits an unhealthy event", func() {
							Eventually(receivedEvents).Should(Receive(Equal(Unhealthy)))
						})

						It("logs the step", func() {
							Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
								"test.monitor-step.transitioned-to-healthy",
								"test.monitor-step.transitioned-to-unhealthy",
							}))
						})

						It("completes with failure", func() {
							Eventually(performErr).Should(Receive(Equal(disaster)))
						})
					})
				})
			})
		})

		Context("when the check is failing immediately", func() {
			BeforeEach(func() {
				checkResults <- errors.New("not up yet!")
			})

			Context("and the start timeout is exceeded", func() {
				BeforeEach(func() {
					startTimeout = 50 * time.Millisecond
					unhealthyInterval = 30 * time.Millisecond
				})

				It("completes with failure", func() {
					expectCheckAfterInterval(unhealthyInterval)
					Consistently(performErr).ShouldNot(Receive())
					expectCheckAfterInterval(unhealthyInterval)
					Eventually(performErr).Should(Receive(MatchError("not up yet!")))
				})

				It("logs the step", func() {
					expectCheckAfterInterval(unhealthyInterval)
					expectCheckAfterInterval(unhealthyInterval)
					Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
						"test.monitor-step.timed-out-before-healthy",
					}))
				})
			})

			Context("and the unhealthy interval passes", func() {
				JustBeforeEach(func() {
					expectCheckAfterInterval(unhealthyInterval)
				})

				It("does not emit an unhealthy event", func() {
					Consistently(receivedEvents).ShouldNot(Receive())
				})

				It("does not exit", func() {
					Consistently(performErr).ShouldNot(Receive())
				})

				Context("and the unhealthy interval passes again", func() {
					JustBeforeEach(func() {
						expectCheckAfterInterval(unhealthyInterval)
					})

					It("does not emit an unhealthy event", func() {
						Consistently(receivedEvents).ShouldNot(Receive())
					})

					It("does not exit", func() {
						Consistently(performErr).ShouldNot(Receive())
					})
				})
			})
		})
	})

	Describe("Cancel", func() {
		It("interrupts the monitoring", func() {
			performResult := make(chan error)
			s := step
			go func() { performResult <- s.Perform() }()
			s.Cancel()
			Eventually(performResult).Should(Receive())
			Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
				"test.monitor-step.cancelling",
			}))
		})
	})
})
