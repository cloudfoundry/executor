package monitor_step_test

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/steps/monitor_step"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("MonitorStep", func() {
	var (
		check          sequence.Step
		checkResults   []error
		interruptCheck chan struct{}

		interval           time.Duration
		healthyThreshold   uint
		unhealthyThreshold uint

		healthyHookURL   *url.URL
		unhealthyHookURL *url.URL

		step sequence.Step

		hookServer *ghttp.Server
	)

	BeforeEach(func() {
		stepSequence := 0

		checkResults = []error{}
		interruptCheck = make(chan struct{})

		check = fake_step.FakeStep{
			WhenPerforming: func() error {
				if len(checkResults) <= stepSequence {
					<-interruptCheck
					return nil
				}

				result := checkResults[stepSequence]

				stepSequence++

				return result
			},
		}

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

	JustBeforeEach(func() {
		step = New(
			check,
			interval,
			healthyThreshold,
			unhealthyThreshold,
			&http.Request{
				Method: "PUT",
				URL:    healthyHookURL,
			},
			&http.Request{
				Method: "PUT",
				URL:    unhealthyHookURL,
			},
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

			AfterEach(func() {
				// unblocking check sequence; opens the floodgates, so ignore any
				// requests after this point
				hookServer.AllowUnhandledRequests = true
				close(interruptCheck)

				step.Cancel()
			})

			Context("when the check succeeds", func() {
				BeforeEach(func() {
					checkResults = append(checkResults, nil)
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				Context("and succeeds again", func() {
					BeforeEach(func() {
						checkResults = append(checkResults, nil)

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

							checkResults = append(checkResults, nil)
							checkResults = append(checkResults, nil)
						})

						It("keeps calm and carries on", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(2))
						})
					})

					Context("and again", func() {
						BeforeEach(func() {
							checkResults = append(checkResults, nil)
						})

						It("hits the healthy endpoint once and only once", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and again", func() {
							BeforeEach(func() {
								checkResults = append(checkResults, nil)

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
					checkResults = append(checkResults, errors.New("nope"))
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
				})

				Context("and fails again", func() {
					BeforeEach(func() {
						checkResults = append(checkResults, errors.New("nope"))

						hookServer.AppendHandlers(
							ghttp.VerifyRequest("PUT", "/unhealthy"),
						)
					})

					It("hits the unhealthy endpoint", func() {
						Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
					})

					Context("and again", func() {
						BeforeEach(func() {
							checkResults = append(checkResults, errors.New("nope"))
						})

						It("hits the unhealthy endpoint once and only once", func() {
							Eventually(hookServer.ReceivedRequests).Should(HaveLen(1))
							Consistently(hookServer.ReceivedRequests).Should(HaveLen(1))
						})

						Context("and again", func() {
							BeforeEach(func() {
								checkResults = append(checkResults, errors.New("nope"))

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
					checkResults = append(checkResults, nil, errors.New("nope"), nil, errors.New("nope"))
				})

				It("does not hit any endpoint", func() {
					Consistently(hookServer.ReceivedRequests()).Should(BeEmpty())
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
