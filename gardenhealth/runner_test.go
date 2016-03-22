package gardenhealth_test

import (
	"errors"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/gardenhealth"
	"github.com/cloudfoundry/dropsonde/metric_sender/fake"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	fakeexecutor "github.com/cloudfoundry-incubator/executor/fakes"
	"github.com/cloudfoundry-incubator/executor/gardenhealth/fakegardenhealth"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type fakeTimer struct {
	TimeChan    chan time.Time
	StopReturns bool
}

func newFakeTimer() *fakeTimer {
	return &fakeTimer{
		TimeChan:    make(chan time.Time),
		StopReturns: true,
	}
}
func (t *fakeTimer) C() <-chan time.Time {
	return t.TimeChan
}

func (*fakeTimer) Reset(time.Duration) bool {
	return true
}
func (t *fakeTimer) Stop() bool {
	return t.StopReturns
}

var _ = Describe("Runner", func() {
	var runner *gardenhealth.Runner
	var process ifrit.Process
	var logger *lagertest.TestLogger
	var checker *fakegardenhealth.FakeChecker
	var executorClient *fakeexecutor.FakeClient
	var timerProvider *fakegardenhealth.FakeTimerProvider
	var checkTimer *fakeTimer
	var timeoutTimer *fakeTimer
	var sender *fake.FakeMetricSender

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		checker = &fakegardenhealth.FakeChecker{}
		executorClient = &fakeexecutor.FakeClient{}
		timerProvider = &fakegardenhealth.FakeTimerProvider{}
		checkTimer = newFakeTimer()
		timeoutTimer = newFakeTimer()

		timers := []clock.Timer{checkTimer, timeoutTimer}
		timerProvider.NewTimerStub = func(time.Duration) clock.Timer {
			timer := timers[0]
			timers = timers[1:]
			return timer
		}

		sender = fake.NewFakeMetricSender()
		metrics.Initialize(sender, nil)
	})

	JustBeforeEach(func() {
		runner = gardenhealth.NewRunner(time.Minute, time.Minute, logger, checker, executorClient, timerProvider)
		process = ifrit.Background(runner)
	})

	AfterEach(func() {
		ginkgomon.Interrupt(process)
	})

	Describe("Run", func() {
		Context("When garden is immediately unhealthy", func() {
			Context("because the health check fails", func() {
				var checkErr = gardenhealth.UnrecoverableError("nope")
				BeforeEach(func() {
					checker.HealthcheckReturns(checkErr)
				})
				It("fails without becoming ready", func() {
					Eventually(process.Wait()).Should(Receive(Equal(checkErr)))
					Consistently(process.Ready()).ShouldNot(BeClosed())
				})

				It("emits a metric for unhealthy cell", func() {
					Eventually(process.Wait()).Should(Receive(Equal(checkErr)))
					Eventually(sender.GetValue("UnhealthyCell").Value).Should(Equal(float64(1)))
				})
			})

			Context("because the health check timed out", func() {
				var blockHealthcheck chan struct{}

				BeforeEach(func() {
					blockHealthcheck = make(chan struct{})
					checker.HealthcheckStub = func(lager.Logger) error {
						<-blockHealthcheck
						return nil
					}
				})

				AfterEach(func() {
					// Send to the channel to eliminate the race
					blockHealthcheck <- struct{}{}
					close(blockHealthcheck)
					blockHealthcheck = nil
				})

				It("fails without becoming ready", func() {
					Eventually(timeoutTimer.TimeChan).Should(BeSent(time.Time{}))
					Eventually(process.Wait()).Should(Receive(Equal(gardenhealth.HealthcheckTimeoutError{})))
					Consistently(process.Ready()).ShouldNot(BeClosed())
				})

				It("emits a metric for unhealthy cell", func() {
					Eventually(timeoutTimer.TimeChan).Should(BeSent(time.Time{}))
					Eventually(process.Wait()).Should(Receive(Equal(gardenhealth.HealthcheckTimeoutError{})))
					Eventually(sender.GetValue("UnhealthyCell").Value).Should(Equal(float64(1)))
				})
			})
		})

		Context("When garden is healthy", func() {
			It("Sets healthy to true only once", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))
				_, healthy := executorClient.SetHealthyArgsForCall(0)
				Expect(healthy).Should(Equal(true))
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Expect(executorClient.SetHealthyCallCount()).To(Equal(1))
			})

			It("Continues to check at the correct interval", func() {
				Eventually(checker.HealthcheckCallCount).Should(Equal(1))
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Eventually(checker.HealthcheckCallCount).Should(Equal(4))
			})

			It("emits a metric for healthy cell", func() {
				Eventually(sender.GetValue("UnhealthyCell").Value).Should(Equal(float64(0)))
			})
		})

		Context("When garden is intermittently healthy", func() {
			var checkErr = errors.New("nope")

			It("Sets healthy to false after it fails, then to true after success and emits respective metrics", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))
				_, healthy := executorClient.SetHealthyArgsForCall(0)
				Expect(healthy).Should(Equal(true))
				Expect(sender.GetValue("UnhealthyCell").Value).To(Equal(float64(0)))

				checker.HealthcheckReturns(checkErr)
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy = executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				Expect(sender.GetValue("UnhealthyCell").Value).To(Equal(float64(1)))

				checker.HealthcheckReturns(nil)
				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(3))
				_, healthy = executorClient.SetHealthyArgsForCall(2)
				Expect(healthy).Should(Equal(true))
				Expect(sender.GetValue("UnhealthyCell").Value).To(Equal(float64(0)))
			})
		})

		Context("When the healthcheck times out", func() {
			var blockHealthcheck chan struct{}

			BeforeEach(func() {
				blockHealthcheck = make(chan struct{})
				checker.HealthcheckStub = func(lager.Logger) error {
					logger.Info("blocking")
					<-blockHealthcheck
					logger.Info("unblocking")
					return nil
				}
			})

			JustBeforeEach(func() {
				Eventually(blockHealthcheck).Should(BeSent(struct{}{}))
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))

				Eventually(checkTimer.TimeChan).Should(BeSent(time.Time{}))
				timeoutTimer.StopReturns = false
				Eventually(timeoutTimer.TimeChan).Should(BeSent(time.Time{}))
			})

			AfterEach(func() {
				close(blockHealthcheck)
			})

			It("sets the executor to unhealthy and emits the unhealthy metric", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy := executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				Eventually(sender.GetValue("UnhealthyCell").Value).Should(Equal(float64(1)))
			})

			It("does not set the executor to healthy when the healtcheck completes", func() {
				blockHealthcheck <- struct{}{}
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				Consistently(executorClient.SetHealthyCallCount).Should(Equal(2))
			})
		})

		Context("When the runner is signaled", func() {

			Context("during the initial health check", func() {
				var blockHealthcheck chan struct{}

				BeforeEach(func() {
					blockHealthcheck = make(chan struct{})
					checker.HealthcheckStub = func(lager.Logger) error {
						<-blockHealthcheck
						return nil
					}
				})

				JustBeforeEach(func() {
					process.Signal(os.Interrupt)
				})

				It("exits with no error", func() {
					Eventually(process.Wait()).Should(Receive(BeNil()))
				})
			})

			Context("After the initial health check", func() {
				It("exits imediately with no error", func() {
					Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))

					process.Signal(os.Interrupt)
					Eventually(process.Wait()).Should(Receive(BeNil()))
				})
			})
		})
	})
})
