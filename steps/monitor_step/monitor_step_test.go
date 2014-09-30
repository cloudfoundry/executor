package monitor_step_test

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/steps/monitor_step"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/pivotal-golang/timer/fake_timer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("MonitorStep", func() {
	var (
		check *fake_step.FakeStep

		healthyHookURL   *url.URL
		unhealthyHookURL *url.URL

		step sequence.Step

		hookServer *ghttp.Server
		logger     *lagertest.TestLogger
		timer      *fake_timer.FakeTimer
	)

	BeforeEach(func() {
		timer = fake_timer.NewFakeTimer(time.Now())
		check = new(fake_step.FakeStep)

		logger = lagertest.NewTestLogger("test")

		hookServer = ghttp.NewServer()

		healthyHookURL = &url.URL{
			Scheme: "http",
			Host:   hookServer.HTTPTestServer.Listener.Addr().String(),
			Path:   "/healthy",
		}

		unhealthyHookURL = &url.URL{
			Scheme: "http",
			Host:   hookServer.HTTPTestServer.Listener.Addr().String(),
			Path:   "/unhealthy",
		}
	})

	Describe("Perform", func() {
		expectCheckAfterInterval := func(d time.Duration) {
			previousCheckCount := check.PerformCallCount()

			timer.Elapse(d - 1*time.Microsecond)
			Consistently(check.PerformCallCount, 0.05).Should(Equal(previousCheckCount))

			timer.Elapse(1 * time.Microsecond)
			Eventually(check.PerformCallCount).Should(Equal(previousCheckCount + 1))
		}

		Context("when the healthy and unhealthy threshold is 2", func() {
			BeforeEach(func() {
				step = New(
					check,
					2,
					2,
					&http.Request{
						Method: "PUT",
						URL:    healthyHookURL,
					},
					&http.Request{
						Method: "PUT",
						URL:    unhealthyHookURL,
					},
					logger,
					timer,
				)
				go step.Perform()
			})

			AfterEach(func() {
				step.Cancel()
			})

			Context("when the check succeeds", func() {
				BeforeEach(func() {
					check.PerformReturns(nil)
					expectCheckAfterInterval(BaseInterval)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				It("checks again after the same interval", func() {
					hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/healthy"))
					expectCheckAfterInterval(BaseInterval)
				})

				Context("when the next check fails", func() {
					BeforeEach(func() {
						check.PerformReturns(errors.New("nope"))
						hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/unhealthy"))
						expectCheckAfterInterval(BaseInterval)
					})

					It("checks again after the base interval", func() {
						expectCheckAfterInterval(BaseInterval)
					})
				})

				Context("when the second check succeeds, but hitting the healthy endpoint fails", func() {
					BeforeEach(func() {
						currentServer := hookServer

						hookServer.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
							currentServer.HTTPTestServer.CloseClientConnections()
						})

						expectCheckAfterInterval(BaseInterval)
					})

					It("keeps calm and carries on", func() {
						Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
					})
				})

				Context("when the second check succeeds", func() {
					BeforeEach(func() {
						check.PerformReturns(nil)
						hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/healthy"))
						expectCheckAfterInterval(BaseInterval)
					})

					It("hits the healthy endpoint", func() {
						Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
					})

					It("checks again after double the interval", func() {
						expectCheckAfterInterval(2 * BaseInterval)
					})

					Context("when the third request succeeds", func() {
						BeforeEach(func() {
							check.PerformReturns(nil)
							expectCheckAfterInterval(BaseInterval * 2)
						})

						It("does not make another request to the healthy endpoint", func() {
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("when the fourth request succeeds", func() {
							BeforeEach(func() {
								hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/healthy"))
								expectCheckAfterInterval(BaseInterval * 4)
							})

							It("hits the healthy endpoint a total of two times", func() {
								Eventually(hookServer.ReceivedRequests).Should(HaveLen(2))
							})

							It("continues to check with an exponential backoff, and eventually reaches a maximum interval", func() {
								hookServer.AllowUnhandledRequests = true
								expectCheckAfterInterval(BaseInterval * 8)
								expectCheckAfterInterval(BaseInterval * 16)
								expectCheckAfterInterval(BaseInterval * 32)
								expectCheckAfterInterval(BaseInterval * 60)

								for i := 0; i < 32; i++ {
									expectCheckAfterInterval(BaseInterval * 60)
								}
							})
						})
					})
				})
			})

			Context("when the check fails", func() {
				BeforeEach(func() {
					check.PerformReturns(errors.New("nope"))
					expectCheckAfterInterval(BaseInterval)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				Context("and fails a second time", func() {
					BeforeEach(func() {
						hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/unhealthy"))
						expectCheckAfterInterval(BaseInterval)
					})

					It("hits the unhealthy endpoint", func() {
						Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
					})

					Context("and fails a third time", func() {
						BeforeEach(func() {
							expectCheckAfterInterval(BaseInterval)
						})

						It("does not hit the unhealthy endpoint again", func() {
							Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and fails a fourth time", func() {
							BeforeEach(func() {
								hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/unhealthy"))
								expectCheckAfterInterval(BaseInterval)
							})

							It("hits the unhealthy endpoint a second time", func() {
								Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(2))
							})
						})
					})
				})
			})

			Context("when the check succeeds, fails, succeeds, and fails", func() {
				BeforeEach(func() {
					check.PerformReturns(nil)
					expectCheckAfterInterval(BaseInterval)

					check.PerformReturns(errors.New("nope"))
					expectCheckAfterInterval(BaseInterval)

					check.PerformReturns(nil)
					expectCheckAfterInterval(BaseInterval)

					check.PerformReturns(errors.New("nope"))
					expectCheckAfterInterval(BaseInterval)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests).Should(BeEmpty())
				})
			})
		})

		Context("when the healthy and unhealthy thresholds are not specified", func() {
			BeforeEach(func() {
				step = New(
					check,
					0,
					0,
					&http.Request{
						Method: "PUT",
						URL:    healthyHookURL,
					},
					&http.Request{
						Method: "PUT",
						URL:    unhealthyHookURL,
					},
					logger,
					timer,
				)

				go step.Perform()
			})

			AfterEach(func() {
				step.Cancel()
			})

			Context("when the check succeeds", func() {
				BeforeEach(func() {
					check.PerformReturns(nil)
					hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/healthy"))
					expectCheckAfterInterval(BaseInterval)
				})

				It("hits the healthy endpoint", func() {
					Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
				})
			})

			Context("when the check fails", func() {
				BeforeEach(func() {
					check.PerformReturns(errors.New("nope"))
					hookServer.AppendHandlers(ghttp.VerifyRequest("PUT", "/unhealthy"))
					expectCheckAfterInterval(BaseInterval)
				})

				It("hits the unhealthy endpoint", func() {
					Eventually(hookServer.ReceivedRequests, 10).Should(HaveLen(1))
				})
			})
		})
	})

	Describe("Cancel", func() {
		BeforeEach(func() {
			step = New(
				check,
				2,
				2,
				&http.Request{
					Method: "PUT",
					URL:    healthyHookURL,
				},
				&http.Request{
					Method: "PUT",
					URL:    unhealthyHookURL,
				},
				logger,
				timer,
			)
		})

		It("interrupts the monitoring", func() {
			performResult := make(chan error)

			go func() { performResult <- step.Perform() }()

			step.Cancel()

			Eventually(performResult).Should(Receive())
		})
	})
})
