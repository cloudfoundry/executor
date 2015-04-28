package client_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	httpclient "github.com/cloudfoundry-incubator/executor/http/client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/onsi/gomega/ghttp"
	"github.com/vito/go-sse/sse"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type fakeTransport struct {
	RoundTripCount int
}

func (t *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.RoundTripCount++
	return nil, errors.New("Something bad happened")
}

var _ = Describe("Client", func() {
	var (
		fakeExecutor *ghttp.Server

		nonStreamingClient *http.Client
		streamingClient    *http.Client

		client executor.Client

		containerGuid string
	)

	action := &models.RunAction{
		Path: "ls",
	}

	BeforeEach(func() {
		containerGuid = "container-guid"
		fakeExecutor = ghttp.NewServer()

		nonStreamingClient = &http.Client{}
		streamingClient = &http.Client{}

		client = httpclient.New(nonStreamingClient, streamingClient, fakeExecutor.URL())
	})

	Describe("Allocate", func() {
		var validRequest []executor.Container
		var validResponse map[string]string

		BeforeEach(func() {
			validRequest = []executor.Container{
				{
					Guid:      containerGuid,
					Action:    action,
					MemoryMB:  64,
					DiskMB:    1024,
					CPUWeight: 5,
					LogConfig: executor.LogConfig{
						Guid:       "some-guid",
						SourceName: "XYZ",
						Index:      0,
					},
				},
			}

			validResponse = map[string]string{}
		})

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers"),
					ghttp.VerifyJSONRepresenting(validRequest),
					ghttp.RespondWithJSONEncoded(http.StatusCreated, validResponse)),
				)
			})

			It("returns a container", func() {
				response, err := client.AllocateContainers(validRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(Equal(validResponse))
			})
		})

		Context("when the call fails because the resources are unavailable", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers"),
					ghttp.RespondWith(http.StatusServiceUnavailable, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.AllocateContainers(validRequest)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the call fails for any other reason", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers"),
					ghttp.RespondWith(http.StatusInternalServerError, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.AllocateContainers(validRequest)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	itRetries := func(m func()) {
		Context("when the executor is down", func() {
			var testTransport *fakeTransport
			BeforeEach(func() {
				testTransport = &fakeTransport{}
				nonStreamingClient.Transport = testTransport
			})

			It("retries the request", func() {
				m()
				Ω(testTransport.RoundTripCount).Should(Equal(2))
			})
		})
	}

	Describe("GetContainer", func() {
		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid),
					ghttp.RespondWith(http.StatusOK, `
          {
						"guid": "guid-123",
						"executor_guid": "executor-guid",
            "memory_mb": 64,
            "disk_mb": 1024,
            "cpu_weight": 5,
            "ports": [
							{ "container_port": 8080, "host_port": 1234 },
							{ "container_port": 8081, "host_port": 1235 }
						],
						"run": {
							"run": {"path": "ls"}
						},
            "log_config": {
              "guid":"some-guid",
              "source_name":"XYZ",
              "index":0
						},
            "metrics_config": {
              "guid":"some-guid",
              "index":0
            }
          }`),
				))
			})

			It("returns a container", func() {
				response, err := client.GetContainer(containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(response).To(Equal(executor.Container{
					Guid: "guid-123",

					MemoryMB:  64,
					DiskMB:    1024,
					CPUWeight: 5,
					Ports: []executor.PortMapping{
						{ContainerPort: 8080, HostPort: 1234},
						{ContainerPort: 8081, HostPort: 1235},
					},
					Action: &models.RunAction{
						Path: "ls",
					},

					LogConfig: executor.LogConfig{
						Guid:       "some-guid",
						SourceName: "XYZ",
						Index:      0,
					},

					MetricsConfig: executor.MetricsConfig{
						Guid:  "some-guid",
						Index: 0,
					},
				}))

			})
		})

		itRetries(func() { client.GetContainer(containerGuid) })

		Context("when the get fails because the container was not found", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid),
					ghttp.RespondWith(executor.ErrContainerNotFound.HttpCode(), "", http.Header{
						"X-Executor-Error": []string{executor.ErrContainerNotFound.Name()},
					})),
				)
			})

			It("returns an error", func() {
				_, err := client.GetContainer(containerGuid)
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when the get fails for some other reason", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid),
					ghttp.RespondWith(http.StatusInternalServerError, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.GetContainer(containerGuid)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("RunContainer", func() {
		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/run"),
					ghttp.RespondWith(http.StatusOK, "")),
				)
			})

			It("does not return an error", func() {
				err := client.RunContainer("guid-123")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the call fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/run"),
					ghttp.RespondWith(executor.ErrStepsInvalid.HttpCode(), "", http.Header{
						"X-Executor-Error": []string{executor.ErrStepsInvalid.Name()},
					})),
				)
			})

			It("returns an error", func() {
				err := client.RunContainer("guid-123")
				Expect(err).To(Equal(executor.ErrStepsInvalid))
			})
		})

		Context("when the executor is down", func() {
			var testTransport *fakeTransport
			BeforeEach(func() {
				testTransport = &fakeTransport{}
				nonStreamingClient.Transport = testTransport
			})

			It("does not retry the request", func() {
				client.RunContainer("guid-123")
				Ω(testTransport.RoundTripCount).Should(Equal(1))
			})
		})
	})

	Describe("DeleteContainer", func() {
		itRetries(func() { client.DeleteContainer("guid-123") })

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("DELETE", "/containers/guid-123"),
					ghttp.RespondWith(http.StatusOK, "")),
				)
			})

			It("does not return an error", func() {
				err := client.DeleteContainer("guid-123")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the call fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("DELETE", "/containers/guid-123"),
					ghttp.RespondWith(http.StatusInternalServerError, "")),
				)
			})

			It("returns an error", func() {
				err := client.DeleteContainer("guid-123")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("ListContainers", func() {
		var listResponse []executor.Container

		BeforeEach(func() {
			listResponse = []executor.Container{
				{Guid: "a", Action: action},
				{Guid: "b", Action: action},
			}
		})

		itRetries(func() { client.ListContainers(nil) })

		Context("with no tags", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers"),
					ghttp.RespondWithJSONEncoded(http.StatusOK, listResponse)),
				)
			})

			It("should returns the list of containers sent back by the server", func() {
				response, err := client.ListContainers(nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(Equal(listResponse))
			})
		})

		Context("when tags are provided", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers"),
					func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Query()["tag"]).To(ConsistOf([]string{"a:b", "c:d"}))
					},
					ghttp.RespondWithJSONEncoded(http.StatusOK, listResponse)),
				)
			})

			It("specifies them as query params", func() {
				response, err := client.ListContainers(executor.Tags{
					"a": "b",
					"c": "d",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(Equal(listResponse))
			})
		})
	})

	Describe("Total Resources", func() {
		var totalResourcesResponse executor.ExecutorResources

		BeforeEach(func() {
			totalResourcesResponse = executor.ExecutorResources{
				MemoryMB:   1024,
				DiskMB:     2048,
				Containers: 32,
			}

			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/resources/total"),
				ghttp.RespondWithJSONEncoded(http.StatusOK, totalResourcesResponse)),
			)
		})

		It("Should returns the total resources", func() {
			response, err := client.TotalResources()
			Expect(err).NotTo(HaveOccurred())
			Expect(response).To(Equal(totalResourcesResponse))
		})

		itRetries(func() { client.TotalResources() })
	})

	Describe("Remaining Resources", func() {
		var remainingResourcesResponse executor.ExecutorResources

		BeforeEach(func() {
			remainingResourcesResponse = executor.ExecutorResources{
				MemoryMB:   1024,
				DiskMB:     2048,
				Containers: 32,
			}

			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/resources/remaining"),
				ghttp.RespondWithJSONEncoded(http.StatusOK, remainingResourcesResponse)),
			)
		})

		It("Should returns the remaining resources", func() {
			response, err := client.RemainingResources()
			Expect(err).NotTo(HaveOccurred())
			Expect(response).To(Equal(remainingResourcesResponse))
		})

		itRetries(func() { client.RemainingResources() })
	})

	Describe("GetFiles", func() {
		var response http.HandlerFunc

		JustBeforeEach(func() {
			v := url.Values{}
			v.Add("source", "path")

			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/containers/guid-123/files", v.Encode()),
				response),
			)
		})

		itRetries(func() { client.GetFiles("guid-123", "path") })

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				response = ghttp.RespondWith(http.StatusOK, "")
			})

			It("does not return an error", func() {
				stream, err := client.GetFiles("guid-123", "path")
				Expect(err).NotTo(HaveOccurred())
				Expect(stream).NotTo(BeNil())
				stream.Close()
			})
		})

		Context("when the call fails", func() {
			BeforeEach(func() {
				response = ghttp.RespondWith(executor.ErrContainerNotFound.HttpCode(), "", http.Header{
					"X-Executor-Error": []string{executor.ErrContainerNotFound.Name()},
				})
			})

			It("returns an error", func() {
				stream, err := client.GetFiles("guid-123", "path")
				Expect(err).To(Equal(executor.ErrContainerNotFound))
				Expect(stream).To(BeNil())
			})
		})
	})

	Describe("GetMetrics", func() {
		var containerGuid string

		itRetries(func() { client.GetMetrics(containerGuid) })

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				containerGuid = "container-guid-1"

				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid+"/metrics"),
					ghttp.RespondWith(http.StatusOK, `
          {
						"memory_usage_in_bytes": 123,
						"disk_usage_in_bytes": 456,
						"time_spent_in_cpu": 789
          }`),
				))
			})

			It("returns the container metrics", func() {
				response, err := client.GetMetrics(containerGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(response).To(Equal(executor.ContainerMetrics{
					MemoryUsageInBytes: 123,
					DiskUsageInBytes:   456,
					TimeSpentInCPU:     789,
				}))

			})
		})

		Context("when the server responds with a non-200 status code", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid+"/metrics"),
					ghttp.RespondWith(executor.ErrContainerNotFound.HttpCode(), "", http.Header{
						"X-Executor-Error": []string{executor.ErrContainerNotFound.Name()},
					})),
				)
			})

			It("returns an error", func() {
				_, err := client.GetMetrics(containerGuid)
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when the server responds with invalid JSON", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid+"/metrics"),
					ghttp.RespondWith(http.StatusOK, `,`),
				))
			})

			It("returns an error", func() {
				_, err := client.GetMetrics(containerGuid)
				Expect(err).To(BeAssignableToTypeOf(&json.SyntaxError{}))
			})
		})
	})

	Describe("GetAllMetrics", func() {
		var allMetricsResponse map[string]executor.Metrics

		BeforeEach(func() {
			allMetricsResponse = map[string]executor.Metrics{
				"a-guid": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: 123,
						DiskUsageInBytes:   456,
						TimeSpentInCPU:     100 * time.Second,
					},
				},
				"b-guid": executor.Metrics{
					MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1},
					ContainerMetrics: executor.ContainerMetrics{
						MemoryUsageInBytes: 321,
						DiskUsageInBytes:   654,
						TimeSpentInCPU:     100 * time.Second,
					},
				},
			}
		})

		itRetries(func() { client.GetAllMetrics(nil) })

		Context("with no tags", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/metrics"),
					ghttp.RespondWithJSONEncoded(http.StatusOK, allMetricsResponse)),
				)
			})

			It("should returns the metrics of all containers", func() {
				response, err := client.GetAllMetrics(nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(Equal(allMetricsResponse))
			})
		})

		Context("with tags", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/metrics"),
					func(w http.ResponseWriter, r *http.Request) {
						Expect(r.URL.Query()["tag"]).To(ConsistOf([]string{"a:b", "c:d"}))
					},
					ghttp.RespondWithJSONEncoded(http.StatusOK, allMetricsResponse)),
				)
			})

			It("specifies them as query params", func() {
				response, err := client.GetAllMetrics(executor.Tags{
					"a": "b",
					"c": "d",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(Equal(allMetricsResponse))
			})
		})

		Context("when the server responds with a non-200 status code", func() {
			Context("when not found", func() {
				BeforeEach(func() {
					fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/metrics"),
						ghttp.RespondWith(executor.ErrContainerNotFound.HttpCode(), "", http.Header{
							"X-Executor-Error": []string{executor.ErrContainerNotFound.Name()},
						})),
					)
				})

				It("returns an error", func() {
					_, err := client.GetAllMetrics(nil)
					Expect(err).To(Equal(executor.ErrContainerNotFound))
				})
			})

			Context("when someother error", func() {
				BeforeEach(func() {
					fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/metrics"),
						ghttp.RespondWith(http.StatusInternalServerError, "", http.Header{
							"X-Executor-Error": []string{executor.ErrContainerNotFound.Name()},
						})),
					)
				})

				It("returns an error", func() {
					_, err := client.GetAllMetrics(nil)
					Expect(err).To(Equal(executor.ErrContainerNotFound))
				})
			})
		})

		Context("when the server responds with invalid JSON", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/metrics"),
					ghttp.RespondWith(http.StatusOK, `,`),
				))
			})

			It("returns an error", func() {
				_, err := client.GetAllMetrics(nil)
				Expect(err).To(BeAssignableToTypeOf(&json.SyntaxError{}))
			})
		})
	})

	Describe("Ping", func() {

		itRetries(func() { client.Ping() })

		Context("when the ping succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/ping"),
					ghttp.RespondWith(http.StatusOK, nil),
				))
			})

			It("should succeed", func() {
				err := client.Ping()
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the ping fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/ping"),
					ghttp.RespondWith(http.StatusBadGateway, nil),
				))
			})

			It("should fail", func() {
				err := client.Ping()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the ping fails with an unrecognized error", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/ping"),
					ghttp.RespondWith(http.StatusBadGateway, nil, http.Header{
						"X-Executor-Error": []string{"Whoa"},
					}),
				))
			})

			It("should fail and at least propagate the original name", func() {
				err := client.Ping()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Whoa"))
			})
		})
	})

	Describe("SubscribeToEvents", func() {
		container1 := executor.Container{
			Guid: "the-guid",

			Action: action,

			RunResult: executor.ContainerRunResult{
				Failed:        true,
				FailureReason: "i hit my head",
			},
		}

		container2 := executor.Container{
			Guid: "a-guid",

			Action: action,

			RunResult: executor.ContainerRunResult{
				Failed: false,
			},
		}

		Context("when the server returns events", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/events"),
					func(w http.ResponseWriter, r *http.Request) {
						flusher := w.(http.Flusher)

						w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
						w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
						w.Header().Add("Connection", "keep-alive")

						w.WriteHeader(http.StatusOK)

						flusher.Flush()

						firstEventPayload, err := json.Marshal(executor.NewContainerCompleteEvent(container1))
						Expect(err).NotTo(HaveOccurred())

						secondEventPayload, err := json.Marshal(executor.NewContainerCompleteEvent(container2))
						Expect(err).NotTo(HaveOccurred())

						thirdEventPayload, err := json.Marshal(executor.NewContainerRunningEvent(container1))
						Expect(err).NotTo(HaveOccurred())

						fourthEventPayload, err := json.Marshal(executor.NewContainerReservedEvent(container1))
						Expect(err).NotTo(HaveOccurred())

						result := sse.Event{
							ID:   "0",
							Name: string(executor.EventTypeContainerComplete),
							Data: firstEventPayload,
						}

						err = result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						flusher.Flush()

						result = sse.Event{
							ID:   "1",
							Name: string(executor.EventTypeContainerComplete),
							Data: secondEventPayload,
						}

						err = result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						flusher.Flush()

						result = sse.Event{
							ID:   "2",
							Name: string(executor.EventTypeContainerRunning),
							Data: thirdEventPayload,
						}

						err = result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						result = sse.Event{
							ID:   "3",
							Name: string(executor.EventTypeContainerReserved),
							Data: fourthEventPayload,
						}

						err = result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						flusher.Flush()
					},
				))
			})

			It("returns them over the channel", func() {
				source, err := client.SubscribeToEvents()
				Expect(err).NotTo(HaveOccurred())

				Expect(source.Next()).To(Equal(executor.NewContainerCompleteEvent(container1)))
				Expect(source.Next()).To(Equal(executor.NewContainerCompleteEvent(container2)))
				Expect(source.Next()).To(Equal(executor.NewContainerRunningEvent(container1)))
				Expect(source.Next()).To(Equal(executor.NewContainerReservedEvent(container1)))

				_, err = source.Next()
				Expect(err).To(Equal(io.EOF))
			})
		})

		Context("when the server returns bogus events", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/events"),
					func(w http.ResponseWriter, r *http.Request) {
						flusher := w.(http.Flusher)

						w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
						w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
						w.Header().Add("Connection", "keep-alive")

						w.WriteHeader(http.StatusOK)

						flusher.Flush()

						result := sse.Event{
							ID:   "0",
							Name: "bogus",
							Data: []byte(":3"),
						}

						err := result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						flusher.Flush()
					},
				))
			})

			It("returns ErrUnknownEventType", func() {
				source, err := client.SubscribeToEvents()
				Expect(err).NotTo(HaveOccurred())

				_, err = source.Next()
				Expect(err).To(Equal(executor.ErrUnknownEventType))
			})
		})

		Context("when the event stream takes a longish time", func() {
			BeforeEach(func() {
				nonStreamingClient.Timeout = 100 * time.Millisecond
				streamingClient.Timeout = 0

				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/events"),
					func(w http.ResponseWriter, r *http.Request) {
						flusher := w.(http.Flusher)

						w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
						w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
						w.Header().Add("Connection", "keep-alive")

						w.WriteHeader(http.StatusOK)

						flusher.Flush()

						firstEventPayload, err := json.Marshal(executor.NewContainerCompleteEvent(container1))
						Expect(err).NotTo(HaveOccurred())

						time.Sleep(2 * nonStreamingClient.Timeout)

						result := sse.Event{
							ID:   "0",
							Name: string(executor.EventTypeContainerComplete),
							Data: firstEventPayload,
						}

						err = result.Write(w)
						Expect(err).NotTo(HaveOccurred())

						flusher.Flush()
					},
				))
			})

			It("does not enforce the non-streaming client's timeout", func() {
				eventSource, err := client.SubscribeToEvents()
				Expect(err).NotTo(HaveOccurred())

				event, err := eventSource.Next()
				Expect(err).NotTo(HaveOccurred())
				Expect(event).To(Equal(executor.NewContainerCompleteEvent(container1)))
			})
		})
	})
})
