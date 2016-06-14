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
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runner", func() {
	var (
		runner                         *gardenhealth.Runner
		process                        ifrit.Process
		logger                         *lagertest.TestLogger
		checker                        *fakegardenhealth.FakeChecker
		executorClient                 *fakeexecutor.FakeClient
		sender                         *fake.FakeMetricSender
		fakeClock                      *fakeclock.FakeClock
		checkInterval, timeoutDuration time.Duration
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		checker = &fakegardenhealth.FakeChecker{}
		executorClient = &fakeexecutor.FakeClient{}
		fakeClock = fakeclock.NewFakeClock(time.Now())
		checkInterval = 2 * time.Minute
		timeoutDuration = 1 * time.Minute

		sender = fake.NewFakeMetricSender()
		metrics.Initialize(sender, nil)
	})

	JustBeforeEach(func() {
		runner = gardenhealth.NewRunner(checkInterval, timeoutDuration, logger, checker, executorClient, fakeClock)
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

				JustBeforeEach(func() {
					fakeClock.WaitForWatcherAndIncrement(timeoutDuration)
				})

				AfterEach(func() {
					// Send to the channel to eliminate the race
					blockHealthcheck <- struct{}{}
					close(blockHealthcheck)
					blockHealthcheck = nil
				})

				It("fails without becoming ready", func() {
					Eventually(process.Wait()).Should(Receive(Equal(gardenhealth.HealthcheckTimeoutError{})))
					Consistently(process.Ready()).ShouldNot(BeClosed())
				})

				It("emits a metric for unhealthy cell", func() {
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
				Expect(executorClient.SetHealthyCallCount()).To(Equal(1))
			})

			It("Continues to check at the correct interval", func() {
				Eventually(checker.HealthcheckCallCount).Should(Equal(1))
				fakeClock.Increment(checkInterval)
				Eventually(checker.HealthcheckCallCount).Should(Equal(2))
				fakeClock.Increment(checkInterval)
				Eventually(checker.HealthcheckCallCount).Should(Equal(3))
				fakeClock.Increment(checkInterval)
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
				fakeClock.WaitForWatcherAndIncrement(checkInterval)

				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy = executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				Expect(sender.GetValue("UnhealthyCell").Value).To(Equal(float64(1)))

				checker.HealthcheckReturns(nil)
				fakeClock.WaitForWatcherAndIncrement(checkInterval)

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

			AfterEach(func() {
				close(blockHealthcheck)
			})

			It("sets the executor to unhealthy and emits the unhealthy metric", func() {
				Eventually(blockHealthcheck).Should(BeSent(struct{}{}))
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))

				fakeClock.WaitForWatcherAndIncrement(checkInterval)
				fakeClock.WaitForWatcherAndIncrement(timeoutDuration)

				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy := executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				Eventually(sender.GetValue("UnhealthyCell").Value).Should(Equal(float64(1)))
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
