package gardenhealth_test

import (
	"errors"
	"os"
	"sync"
	"time"

	"code.cloudfoundry.org/executor/gardenhealth"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"code.cloudfoundry.org/clock/fakeclock"
	fakeexecutor "code.cloudfoundry.org/executor/fakes"
	"code.cloudfoundry.org/executor/gardenhealth/fakegardenhealth"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	mfakes "code.cloudfoundry.org/loggregator_v2/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Runner", func() {
	var (
		runner                          *gardenhealth.Runner
		process                         ifrit.Process
		logger                          *lagertest.TestLogger
		checker                         *fakegardenhealth.FakeChecker
		executorClient                  *fakeexecutor.FakeClient
		fakeClock                       *fakeclock.FakeClock
		fakeMetronClient                *mfakes.FakeClient
		checkInterval, emissionInterval time.Duration
		timeoutDuration                 time.Duration
		metricMap                       map[string]float64
		m                               sync.RWMutex
		createSendMetricStub            func(map[string]float64)
	)

	const UnhealthyCell = "UnhealthyCell"

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		checker = &fakegardenhealth.FakeChecker{}
		executorClient = &fakeexecutor.FakeClient{}
		fakeClock = fakeclock.NewFakeClock(time.Now())
		checkInterval = 2 * time.Minute
		timeoutDuration = 1 * time.Minute
		emissionInterval = 30 * time.Second

		fakeMetronClient = new(mfakes.FakeClient)

		m = sync.RWMutex{}
		createSendMetricStub = func(metricMap map[string]float64) {
			m.Lock()
			fakeMetronClient.SendMetricStub = func(name string, value int) error {
				m.Lock()
				metricMap[name] = float64(value)
				m.Unlock()
				return nil
			}
			m.Unlock()
		}
	})

	JustBeforeEach(func() {
		runner = gardenhealth.NewRunner(checkInterval, emissionInterval, timeoutDuration, logger, checker, executorClient, fakeMetronClient, fakeClock)
		process = ifrit.Background(runner)

		m.Lock()
		metricMap = make(map[string]float64)
		m.Unlock()
		createSendMetricStub(metricMap)
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
					executorClient.HealthyReturns(false)
				})

				It("fails without becoming ready", func() {
					Eventually(process.Wait()).Should(Receive(Equal(checkErr)))
					Consistently(process.Ready()).ShouldNot(BeClosed())
				})

				It("emits a metric for unhealthy cell", func() {
					Eventually(process.Wait()).Should(Receive(Equal(checkErr)))
					m.RLock()
					Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
					m.RUnlock()
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
					m.RLock()
					Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
					m.RUnlock()
				})
			})
		})

		Context("When garden is healthy", func() {
			BeforeEach(func() {
				executorClient.HealthyReturns(true)
			})

			It("sets healthy to true only once", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))
				_, healthy := executorClient.SetHealthyArgsForCall(0)
				Expect(healthy).Should(Equal(true))
				Expect(executorClient.SetHealthyCallCount()).To(Equal(1))
			})

			It("continues to check at the correct interval", func() {
				Eventually(checker.HealthcheckCallCount).Should(Equal(1))
				Eventually(process.Ready()).Should(BeClosed())

				fakeClock.WaitForNWatchersAndIncrement(checkInterval, 2)
				Eventually(checker.HealthcheckCallCount).Should(Equal(2))
				Eventually(logger).Should(gbytes.Say("check-complete"))

				fakeClock.WaitForNWatchersAndIncrement(checkInterval, 2)
				Eventually(checker.HealthcheckCallCount).Should(Equal(3))
				Eventually(logger).Should(gbytes.Say("check-complete"))

				fakeClock.WaitForNWatchersAndIncrement(checkInterval, 2)
				Eventually(checker.HealthcheckCallCount).Should(Equal(4))
				Eventually(logger).Should(gbytes.Say("check-complete"))
			})

			It("emits a metric for healthy cell", func() {
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(0)))
				m.RUnlock()
			})
		})

		Context("when garden is intermittently healthy", func() {
			var (
				checkValues   chan error
				healthyValues chan bool
			)

			BeforeEach(func() {
				healthyValues = make(chan bool, 1)
				checkValues = make(chan error, 1)
				executorClient.HealthyStub = func(lager.Logger) bool {
					return <-healthyValues
				}
				checker.HealthcheckStub = func(lager.Logger) error {
					return <-checkValues
				}

				Expect(healthyValues).To(BeSent(true))
				checkValues <- nil

				// Set emission interval to a high value so that it doesn't trigger in this test
				emissionInterval = 100 * time.Minute
			})

			It("Sets healthy to false after it fails, then to true after success and emits respective metrics", func() {
				Eventually(executorClient.SetHealthyCallCount).Should(Equal(1))
				_, healthy := executorClient.SetHealthyArgsForCall(0)
				Expect(healthy).Should(Equal(true))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(0)))
				m.RUnlock()

				Expect(healthyValues).To(BeSent(false))
				checkValues <- errors.New("boom")
				fakeClock.WaitForWatcherAndIncrement(checkInterval)

				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy = executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
				m.RUnlock()

				Expect(healthyValues).To(BeSent(true))
				checkValues <- nil
				fakeClock.WaitForNWatchersAndIncrement(checkInterval, 2)

				Eventually(executorClient.SetHealthyCallCount).Should(Equal(3))
				_, healthy = executorClient.SetHealthyArgsForCall(2)
				Expect(healthy).Should(Equal(true))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(0)))
				m.RUnlock()
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

				fakeClock.WaitForNWatchersAndIncrement(checkInterval, 2)
				Eventually(checker.HealthcheckCallCount).Should(Equal(2))

				fakeClock.WaitForNWatchersAndIncrement(timeoutDuration, 2)

				Eventually(executorClient.SetHealthyCallCount).Should(Equal(2))
				_, healthy := executorClient.SetHealthyArgsForCall(1)
				Expect(healthy).Should(Equal(false))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
				m.RUnlock()
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

		Context("UnhealthyCell metric emission", func() {
			It("emits the UnhealthyCell every emitInterval", func() {
				Eventually(executorClient.HealthyCallCount).Should(Equal(1))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
				m.RUnlock()

				executorClient.HealthyReturns(true)
				fakeClock.WaitForWatcherAndIncrement(emissionInterval)

				Eventually(executorClient.HealthyCallCount).Should(Equal(2))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(0)))
				m.RUnlock()

				executorClient.HealthyReturns(false)
				fakeClock.WaitForWatcherAndIncrement(emissionInterval)

				Eventually(executorClient.HealthyCallCount).Should(Equal(3))
				m.RLock()
				Eventually(metricMap).Should(HaveKeyWithValue(UnhealthyCell, float64(1)))
				m.RUnlock()
			})
		})
	})
})
