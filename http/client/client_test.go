package client_test

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor"
	httpclient "github.com/cloudfoundry-incubator/executor/http/client"
	"github.com/onsi/gomega/ghttp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	var fakeExecutor *ghttp.Server
	var client executor.Client
	var containerGuid string

	BeforeEach(func() {
		containerGuid = "container-guid"
		fakeExecutor = ghttp.NewServer()
		client = httpclient.New(http.DefaultClient, fakeExecutor.URL())
	})

	Describe("Allocate", func() {
		var validRequest executor.Container
		var validResponse executor.Container

		BeforeEach(func() {
			zero := 0

			validRequest = executor.Container{
				Guid:      containerGuid,
				MemoryMB:  64,
				DiskMB:    1024,
				CPUWeight: 5,
				Log: executor.LogConfig{
					Guid:       "some-guid",
					SourceName: "XYZ",
					Index:      &zero,
				},
			}

			validResponse = executor.Container{
				Guid:     "guid-123",
				MemoryMB: 64,
				DiskMB:   1024,
			}
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
				response, err := client.AllocateContainer(validRequest)

				Ω(err).ShouldNot(HaveOccurred())
				Ω(response).Should(Equal(validResponse))
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
				_, err := client.AllocateContainer(validRequest)
				Ω(err).Should(HaveOccurred())
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
				_, err := client.AllocateContainer(validRequest)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("Get", func() {
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
            "log": {
              "guid":"some-guid",
              "source_name":"XYZ",
              "index":0
            }
          }`),
				))
			})

			It("returns a container", func() {
				response, err := client.GetContainer(containerGuid)
				Ω(err).ShouldNot(HaveOccurred())

				zero := 0
				Ω(response).Should(Equal(executor.Container{
					Guid: "guid-123",

					MemoryMB:  64,
					DiskMB:    1024,
					CPUWeight: 5,
					Ports: []executor.PortMapping{
						{ContainerPort: 8080, HostPort: 1234},
						{ContainerPort: 8081, HostPort: 1235},
					},

					Log: executor.LogConfig{
						Guid:       "some-guid",
						SourceName: "XYZ",
						Index:      &zero,
					},
				}))
			})
		})

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
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
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
				Ω(err).Should(HaveOccurred())
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
				Ω(err).ShouldNot(HaveOccurred())
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
				Ω(err).Should(Equal(executor.ErrStepsInvalid))
			})
		})
	})

	Describe("DeleteContainer", func() {
		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("DELETE", "/containers/guid-123"),
					ghttp.RespondWith(http.StatusOK, "")),
				)
			})

			It("does not return an error", func() {
				err := client.DeleteContainer("guid-123")
				Ω(err).ShouldNot(HaveOccurred())
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
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("ListContainers", func() {
		var listResponse []executor.Container

		BeforeEach(func() {
			listResponse = []executor.Container{
				{Guid: "a"},
				{Guid: "b"},
			}
		})

		Context("with no tags", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers"),
					ghttp.RespondWithJSONEncoded(http.StatusOK, listResponse)),
				)
			})

			It("should returns the list of containers sent back by the server", func() {
				response, err := client.ListContainers(nil)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(response).Should(Equal(listResponse))
			})
		})

		Context("when tags are provided", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers"),
					func(w http.ResponseWriter, r *http.Request) {
						Ω(r.URL.Query()["tag"]).Should(ConsistOf([]string{"a:b", "c:d"}))
					},
					ghttp.RespondWithJSONEncoded(http.StatusOK, listResponse)),
				)
			})

			It("specifies them as query params", func() {
				response, err := client.ListContainers(executor.Tags{
					"a": "b",
					"c": "d",
				})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(response).Should(Equal(listResponse))
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
			Ω(err).ShouldNot(HaveOccurred())
			Ω(response).Should(Equal(totalResourcesResponse))
		})
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
			Ω(err).ShouldNot(HaveOccurred())
			Ω(response).Should(Equal(remainingResourcesResponse))
		})
	})

	Describe("Ping", func() {
		Context("when the ping succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/ping"),
					ghttp.RespondWith(http.StatusOK, nil),
				))
			})

			It("should succeed", func() {
				err := client.Ping()
				Ω(err).ShouldNot(HaveOccurred())
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
				Ω(err).Should(HaveOccurred())
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
				Ω(err).Should(HaveOccurred())
				Ω(err.Error()).Should(ContainSubstring("Whoa"))
			})
		})
	})
})
