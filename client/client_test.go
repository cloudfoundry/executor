package client_test

import (
	. "github.com/cloudfoundry-incubator/executor/client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/onsi/gomega/ghttp"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	var fakeExecutor *ghttp.Server
	var client Client

	BeforeEach(func() {
		fakeExecutor = ghttp.NewServer()
		client = New(http.DefaultClient, fakeExecutor.URL())
	})

	Describe("Allocate", func() {
		var validRequest ContainerRequest

		BeforeEach(func() {
			zero := 0
			validRequest = ContainerRequest{
				MemoryMB:   64,
				DiskMB:     1024,
				CpuPercent: 0.5,
				LogConfig: models.LogConfig{
					Guid:       "some-guid",
					SourceName: "XYZ",
					Index:      &zero,
				},
			}
		})

		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers"),
					ghttp.VerifyJSON(`
          {
            "memory_mb": 64,
            "disk_mb": 1024,
            "cpu_percent": 0.5,
            "log": {
              "guid":"some-guid",
              "source_name":"XYZ",
              "index":0
            }
          }`),
					ghttp.RespondWith(http.StatusCreated, `{"executor_guid":"executor-guid","guid":"guid-123"}`)),
				)
			})

			It("returns a container", func() {
				response, err := client.AllocateContainer(validRequest)

				Ω(err).ShouldNot(HaveOccurred())
				Ω(response).Should(Equal(ContainerResponse{
					ContainerRequest: validRequest,
					Guid:             "guid-123",
					ExecutorGuid:     "executor-guid",
				}))
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

	Describe("InitializeContainer", func() {
		Context("when the call succeeds", func() {
			BeforeEach(func() {
				fakeExecutor.AppendHandlers(ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/containers/guid-123/initialize"),
					ghttp.RespondWith(http.StatusCreated, "")),
				)
			})

			It("does not return an error", func() {
				err := client.InitializeContainer("guid-123")
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
				err := client.InitializeContainer("guid-123")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("Run", func() {
		var validRequest RunRequest

		BeforeEach(func() {
			validRequest = RunRequest{
				Metadata:      []byte("abc"), // base64-encoded in JSON
				CompletionURL: "the-completion-url",
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
})
