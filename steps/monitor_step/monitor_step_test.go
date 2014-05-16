package monitor_step_test

import (
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/steps/monitor_step"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

type FakeHealthCheck struct {
	results  []bool
	sequence int
}

func NewFakeHealthCheck() *FakeHealthCheck {
	return &FakeHealthCheck{}
}

func (check *FakeHealthCheck) Check() bool {
	if len(check.results) <= check.sequence {
		select {}
	}

	result := check.results[check.sequence]

	check.sequence++

	return result
}

func (check *FakeHealthCheck) QueueResult(healthy bool) {
	check.results = append(check.results, healthy)
}

var _ = Describe("MonitorStep", func() {
	var (
		check *FakeHealthCheck

		interval           time.Duration
		healthyThreshold   uint
		unhealthyThreshold uint

		healthyHook   *url.URL
		unhealthyHook *url.URL

		step sequence.Step

		hookServer *ghttp.Server
	)

	BeforeEach(func() {
		check = NewFakeHealthCheck()
		hookServer = ghttp.NewServer()

		healthyHook = &url.URL{
			Scheme: "http",
			Host:   hookServer.HTTPTestServer.Listener.Addr().String(),
			Path:   "/healthy",
		}

		unhealthyHook = &url.URL{
			Scheme: "http",
			Host:   hookServer.HTTPTestServer.Listener.Addr().String(),
			Path:   "/unhealthy",
		}
	})

	JustBeforeEach(func() {
		step = New(
			check,
			interval,
			healthyThreshold,
			unhealthyThreshold,
			healthyHook,
			unhealthyHook,
		)
	})

	Describe("Perform", func() {
		Context("when the healthy and unhealthy threshold is 2", func() {
			BeforeEach(func() {
				healthyThreshold = 2
				unhealthyThreshold = 2
				interval = 1 * time.Nanosecond
			})

			JustBeforeEach(func() {
				go step.Perform()
			})

			Context("when the check succeeds", func() {
				BeforeEach(func() {
					check.QueueResult(true)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				Context("and succeeds again", func() {
					BeforeEach(func() {
						check.QueueResult(true)

						hookServer.AppendHandlers(
							ghttp.VerifyRequest("PUT", "/healthy"),
						)
					})

					It("hits the healthy endpoint", func() {
						Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
					})

					Context("when hitting the endpoint fails", func() {
						BeforeEach(func() {
							hookServer.SetHandler(0, func(w http.ResponseWriter, r *http.Request) {
								hookServer.HTTPTestServer.CloseClientConnections()
							})

							hookServer.AppendHandlers(
								ghttp.VerifyRequest("PUT", "/healthy"),
							)

							check.QueueResult(true)
							check.QueueResult(true)
						})

						It("keeps calm and carries on", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(2))
						})
					})

					Context("and again", func() {
						BeforeEach(func() {
							check.QueueResult(true)
						})

						It("hits the healthy endpoint once and only once", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and again", func() {
							BeforeEach(func() {
								check.QueueResult(true)

								hookServer.AppendHandlers(
									ghttp.VerifyRequest("PUT", "/healthy"),
								)
							})

							It("hits the healthy endpoint a total of two times", func() {
								Eventually(hookServer.ReceivedRequests).Should(HaveLen(2))
							})
						})
					})
				})
			})

			Context("when the check fails", func() {
				BeforeEach(func() {
					check.QueueResult(false)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				Context("and fails again", func() {
					BeforeEach(func() {
						check.QueueResult(false)

						hookServer.AppendHandlers(
							ghttp.VerifyRequest("PUT", "/unhealthy"),
						)
					})

					It("hits the unhealthy endpoint", func() {
						Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
					})

					Context("and again", func() {
						BeforeEach(func() {
							check.QueueResult(false)
						})

						It("hits the unhealthy endpoint once and only once", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and again", func() {
							BeforeEach(func() {
								check.QueueResult(false)

								hookServer.AppendHandlers(
									ghttp.VerifyRequest("PUT", "/unhealthy"),
								)
							})

							It("hits the unhealthy endpoint a total of two times", func() {
								Eventually(hookServer.ReceivedRequests).Should(HaveLen(2))
							})
						})
					})
				})
			})

			Context("when the check succeeds, fails, succeeds, and fails", func() {
				BeforeEach(func() {
					check.QueueResult(true)
					check.QueueResult(false)
					check.QueueResult(true)
					check.QueueResult(false)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})
			})
		})
	})
})
