package client_test

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/onsi/gomega/ghttp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	var fakeExecutor *ghttp.Server
	var client Client
	var containerGuid string

	BeforeEach(func() {
		containerGuid = "container-guid"
		fakeExecutor = ghttp.NewServer()
		client = New(http.DefaultClient, fakeExecutor.URL())
	})

	Describe("Allocate", func() {
		var validRequest api.ContainerAllocationRequest
		var validResponse api.Container

		BeforeEach(func() {
			validRequest = api.ContainerAllocationRequest{
				MemoryMB: 64,
				DiskMB:   1024,
			}
			validResponse = api.Container{
				Guid:         "guid-123",
				ExecutorGuid: "executor-guid",
				MemoryMB:     64,
				DiskMB:       1024,
			}
		})

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/"+containerGuid),
					ghttp.VerifyJSON(`
          {
            "memory_mb": 64,
            "disk_mb": 1024,
            "metadata": null
          }`),
					ghttp.RespondWithJSONEncoded(http.StatusCreated, validResponse)),
				)
			})

			It("returns a container", func() {
				response, err := client.AllocateContainer(containerGuid, validRequest)

				Ω(err).ShouldNot(HaveOccurred())
				Ω(response).Should(Equal(validResponse))
			})
		})

		Context("when the call fails because the resources are unavailable", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/"+containerGuid),
					ghttp.RespondWith(http.StatusServiceUnavailable, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.AllocateContainer(containerGuid, validRequest)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the call fails for any other reason", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/"+containerGuid),
					ghttp.RespondWith(http.StatusInternalServerError, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.AllocateContainer(containerGuid, validRequest)
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
            "cpu_percent": 0.5,
            "ports": [
							{ "container_port": 8080, "host_port": 1234 },
							{ "container_port": 8081, "host_port": 1235 }
						],
            "metadata": null,
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
				Ω(response).Should(Equal(api.Container{
					Guid: "guid-123",

					MemoryMB:   64,
					DiskMB:     1024,
					CpuPercent: 0.5,
					Ports: []api.PortMapping{
						{ContainerPort: 8080, HostPort: 1234},
						{ContainerPort: 8081, HostPort: 1235},
					},

					Log: models.LogConfig{
						Guid:       "some-guid",
						SourceName: "XYZ",
						Index:      &zero,
					},

					ExecutorGuid: "executor-guid",
				}))
			})
		})

		Context("when the get fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/containers/"+containerGuid),
					ghttp.RespondWith(http.StatusNotFound, "")),
				)
			})

			It("returns an error", func() {
				_, err := client.GetContainer(containerGuid)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("InitializeContainer", func() {
		var validRequest api.ContainerInitializationRequest

		BeforeEach(func() {
			zero := 0
			validRequest = api.ContainerInitializationRequest{
				CpuPercent: 0.5,
				Log: models.LogConfig{
					Guid:       "some-guid",
					SourceName: "XYZ",
					Index:      &zero,
				},
			}
		})

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/initialize"),
					ghttp.VerifyJSON(`
          {
            "cpu_percent": 0.5,
            "ports": null,
            "log": {
              "guid":"some-guid",
              "source_name":"XYZ",
              "index":0
            }
          }`),
					ghttp.RespondWith(http.StatusCreated, "")),
				)
			})

			It("does not return an error", func() {
				err := client.InitializeContainer("guid-123", validRequest)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the call fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/initialize"),
					ghttp.RespondWith(http.StatusInternalServerError, "")),
				)
			})

			It("returns an error", func() {
				err := client.InitializeContainer("guid-123", validRequest)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("Run", func() {
		var validRequest api.ContainerRunRequest

		BeforeEach(func() {
			validRequest = api.ContainerRunRequest{
				Metadata:    []byte("abc"), // base64-encoded in JSON
				CompleteURL: "the-completion-url",
				Actions: []models.ExecutorAction{
					{
						Action: models.RunAction{
							Script:  "the-script",
							Env:     []models.EnvironmentVariable{{Key: "PATH", Value: "the-path"}},
							Timeout: time.Second,
						},
					},
				},
			}
		})

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyJSON(`
            {
              "actions": [
                {
                  "action":"run",
                  "args":{
                    "script":"the-script",
                    "env":[{"key":"PATH","value":"the-path"}],
                    "timeout":1000000000,
                    "resource_limits":{}
                  }
                }
              ],
              "metadata":"YWJj",
              "complete_url":"the-completion-url"
            }
          `),
					ghttp.VerifyRequest("POST", "/containers/guid-123/run"),
					ghttp.RespondWith(http.StatusOK, "")),
				)
			})

			It("does not return an error", func() {
				err := client.Run("guid-123", validRequest)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the call fails", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/run"),
					ghttp.RespondWith(http.StatusInternalServerError, "")))
			})

			It("returns an error", func() {
				err := client.Run("guid-123", validRequest)
				Ω(err).Should(HaveOccurred())
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
		var listResponse []api.Container
		BeforeEach(func() {
			listResponse = []api.Container{
				{Guid: "a"},
				{Guid: "b"},
			}

			fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/containers"),
				ghttp.RespondWithJSONEncoded(http.StatusOK, listResponse)),
			)
		})

		It("should returns the list of containers sent back by the server", func() {
			response, err := client.ListContainers()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(response).Should(Equal(listResponse))
		})
	})

	Describe("Total Resources", func() {
		var totalResourcesResponse api.ExecutorResources

		BeforeEach(func() {
			totalResourcesResponse = api.ExecutorResources{
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
		var remainingResourcesResponse api.ExecutorResources

		BeforeEach(func() {
			remainingResourcesResponse = api.ExecutorResources{
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

})
