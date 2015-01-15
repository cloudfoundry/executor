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
		fakeStep1 *fakes.FakeStep
		fakeStep2 *fakes.FakeStep

		checkSteps chan *fakes.FakeStep

		checkFunc        func() Step
		hasBecomeHealthy <-chan struct{}
		timeProvider     *faketimeprovider.FakeTimeProvider

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

		fakeStep1 = new(fakes.FakeStep)
		fakeStep2 = new(fakes.FakeStep)

		checkSteps = make(chan *fakes.FakeStep, 2)
		checkSteps <- fakeStep1
		checkSteps <- fakeStep2

		timeProvider = faketimeprovider.New(time.Now())

		checkFunc = func() Step {
			return <-checkSteps
		}

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		hasBecomeHealthyChannel := make(chan struct{}, 1000)
		hasBecomeHealthy = hasBecomeHealthyChannel

		step = NewMonitor(
			checkFunc,
			hasBecomeHealthyChannel,
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

		expectCheckAfterInterval := func(fakeStep *fakes.FakeStep, d time.Duration) {
			previousCheckCount := fakeStep.PerformCallCount()

			timeProvider.Increment(d - 1*time.Microsecond)
			Consistently(fakeStep.PerformCallCount, 0.05).Should(Equal(previousCheckCount))

			timeProvider.Increment(d)
			Eventually(fakeStep.PerformCallCount).Should(Equal(previousCheckCount + 1))
		}

		BeforeEach(func() {
			results := make(chan error, 10)
			checkResults = results

			var currentResult error

			fakedResult := func() error {
				select {
				case currentResult = <-results:
				default:
				}

				return currentResult
			}

			fakeStep1.PerformStub = fakedResult
			fakeStep2.PerformStub = fakedResult
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
					expectCheckAfterInterval(fakeStep1, unhealthyInterval)
				})

				It("emits a healthy event", func() {
					Eventually(hasBecomeHealthy).Should(Receive())
				})

				It("logs the step", func() {
					Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
						"test.monitor-step.transitioned-to-healthy",
					}))
				})

				Context("and the healthy interval passes", func() {
					JustBeforeEach(func() {
						Eventually(hasBecomeHealthy).Should(Receive())
						expectCheckAfterInterval(fakeStep2, healthyInterval)
					})

					It("does not emit another healthy event", func() {
						Consistently(hasBecomeHealthy).ShouldNot(Receive())
					})
				})

				Context("and the check begins to fail", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						checkResults <- disaster
					})

					Context("and the healthy interval passes", func() {
						JustBeforeEach(func() {
							Eventually(hasBecomeHealthy).Should(Receive())
							expectCheckAfterInterval(fakeStep2, healthyInterval)
						})

						It("emits nothing", func() {
							Consistently(hasBecomeHealthy).ShouldNot(Receive())
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
					expectCheckAfterInterval(fakeStep1, unhealthyInterval)
					Consistently(performErr).ShouldNot(Receive())
					expectCheckAfterInterval(fakeStep2, unhealthyInterval)
					Eventually(performErr).Should(Receive(MatchError("not up yet!")))
				})

				It("logs the step", func() {
					expectCheckAfterInterval(fakeStep1, unhealthyInterval)
					expectCheckAfterInterval(fakeStep2, unhealthyInterval)
					Eventually(logger.TestSink.LogMessages).Should(ConsistOf([]string{
						"test.monitor-step.timed-out-before-healthy",
					}))
				})
			})

			Context("and the unhealthy interval passes", func() {
				JustBeforeEach(func() {
					expectCheckAfterInterval(fakeStep1, unhealthyInterval)
				})

				It("does not emit an unhealthy event", func() {
					Consistently(hasBecomeHealthy).ShouldNot(Receive())
				})

				It("does not exit", func() {
					Consistently(performErr).ShouldNot(Receive())
				})

				Context("and the unhealthy interval passes again", func() {
					JustBeforeEach(func() {
						expectCheckAfterInterval(fakeStep2, unhealthyInterval)
					})

					It("does not emit an unhealthy event", func() {
						Consistently(hasBecomeHealthy).ShouldNot(Receive())
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
		})
	})
})
