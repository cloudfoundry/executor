package steps_test

import (
	"errors"
	"sync"
	"time"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/pivotal-golang/timer/fake_timer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("MonitorStep", func() {
	var (
		check          *fakes.FakeStep
		receivedEvents <-chan HealthEvent
		timer          *fake_timer.FakeTimer

		healthyInterval   = 1 * time.Second
		unhealthyInterval = 500 * time.Millisecond

		step Step
	)

	BeforeEach(func() {
		timer = fake_timer.NewFakeTimer(time.Now())
		check = new(fakes.FakeStep)

		events := make(chan HealthEvent, 1000)
		receivedEvents = events

		step = NewMonitor(
			check,
			events,
			lagertest.NewTestLogger("test"),
			timer,
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

			timer.Elapse(d - 1*time.Microsecond)
			Consistently(check.PerformCallCount, 0.05).Should(Equal(previousCheckCount))

			timer.Elapse(1 * time.Microsecond)
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

			go func() { performResult <- step.Perform() }()

			step.Cancel()

			Eventually(performResult).Should(Receive())
		})
	})
})
