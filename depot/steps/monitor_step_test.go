package steps_test

import (
	"errors"
	"sync"
	"time"


	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"
	. "github.com/cloudfoundry-incubator/executor/depot/steps"
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

		healthyInterval   = 500 * time.Microsecond
		unhealthyInterval = 1 * time.Microsecond

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
				check.PerformReturns(nil)
			})

			Context("and the unhealthy interval passes", func() {
				BeforeEach(func() {
					expectCheckAfterInterval(unhealthyInterval)
				})

				It("emits a healthy event", func() {
					Eventually(receivedEvents).Should(Receive(Equal(Healthy)))
				})

				Context("and the healthy interval passes", func() {
					BeforeEach(func() {
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
						check.PerformReturns(disaster)
					})

					Context("and the healthy interval passes", func() {
						BeforeEach(func() {
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
				check.PerformReturns(errors.New("not up yet!"))
			})

			Context("and the unhealthy interval passes", func() {
				BeforeEach(func() {
					expectCheckAfterInterval(unhealthyInterval)
				})

				It("does not emit an unhealthy event", func() {
					Consistently(receivedEvents).ShouldNot(Receive())
				})

				It("does not exit", func() {
					Consistently(performErr).ShouldNot(Receive())
				})

				Context("and the unhealthy interval passes again", func() {
					BeforeEach(func() {
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
