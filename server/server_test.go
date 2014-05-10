package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	Registry "github.com/cloudfoundry-incubator/executor/registry"
	. "github.com/cloudfoundry-incubator/executor/server"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
	"github.com/pivotal-golang/cacheddownloader/fakecacheddownloader"
	"github.com/tedsuo/router"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

func MarshalledPayload(payload interface{}) io.Reader {
	reqBody, err := json.Marshal(payload)
	Ω(err).ShouldNot(HaveOccurred())

	return bytes.NewBuffer(reqBody)
}

var _ = Describe("Api", func() {
	var registry Registry.Registry
	var wardenClient *fake_warden_client.FakeClient

	var server *httptest.Server
	var generator *router.RequestGenerator

	BeforeEach(func() {
		wardenClient = fake_warden_client.New()

		registry = Registry.New("executor-guid-123", Registry.Capacity{
			MemoryMB:   1024,
			DiskMB:     1024,
			Containers: 1024,
		})

		gosteno.EnterTestMode(gosteno.LOG_DEBUG)
		logger := gosteno.NewLogger("api-test-logger")

		handler, err := New(&Config{
			Registry: registry,
			Transformer: transformer.NewTransformer(
				log_streamer_factory.New("", ""),
				fakecacheddownloader.New(),
				fake_uploader.New(),
				&fake_extractor.FakeExtractor{},
				&fake_compressor.FakeCompressor{},
				logger,
				"/tmp",
			),
			WardenClient:          wardenClient,
			ContainerOwnerName:    "some-container-owner-name",
			ContainerMaxCPUShares: 1024,
			WaitGroup:             &sync.WaitGroup{},
			Logger:                logger,
		})

		Ω(err).ShouldNot(HaveOccurred())

		server = httptest.NewServer(handler)

		generator = router.NewRequestGenerator("http://"+server.Listener.Addr().String(), api.Routes)
	})

	AfterEach(func() {
		server.Close()
	})

	DoRequest := func(req *http.Request, err error) *http.Response {
		Ω(err).ShouldNot(HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		Ω(err).ShouldNot(HaveOccurred())

		return resp
	}

	Describe("POST /containers", func() {
		var reserveRequestBody io.Reader
		var reserveResponse *http.Response

		BeforeEach(func() {
			reserveRequestBody = nil
			reserveResponse = nil
		})

		JustBeforeEach(func() {
			reserveResponse = DoRequest(generator.RequestForHandler(
				api.AllocateContainer,
				nil,
				reserveRequestBody,
			))
		})

		Context("when the requested CPU percent is > 100", func() {
			BeforeEach(func() {
				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: 101.0,
				})
			})

			It("returns 400", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when the requested CPU percent is < 0", func() {
			BeforeEach(func() {
				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: -14.0,
				})
			})

			It("returns 400", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when there are containers available", func() {
			var reservedContainer api.Container

			BeforeEach(func() {
				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: 0.5,
				})
			})

			JustBeforeEach(func() {
				err := json.NewDecoder(reserveResponse.Body).Decode(&reservedContainer)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns 201", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusCreated))
			})

			It("returns a container", func() {
				Ω(reservedContainer).Should(Equal(api.Container{
					ExecutorGuid: "executor-guid-123",
					Guid:         reservedContainer.Guid,
					MemoryMB:     64,
					DiskMB:       512,
					CpuPercent:   0.5,
					State:        "reserved",
				}))
				Ω(reservedContainer.Guid).ShouldNot(Equal(""))
			})

			Context("and we request an available container", func() {
				var getResponse *http.Response

				JustBeforeEach(func() {
					getResponse = DoRequest(generator.RequestForHandler(
						api.GetContainer,
						router.Params{"guid": reservedContainer.Guid},
						nil,
					))
				})

				AfterEach(func() {
					getResponse.Body.Close()
				})

				It("returns 200 OK", func() {
					Ω(getResponse.StatusCode).Should(Equal(http.StatusOK))
				})

				It("retains the reserved containers", func() {
					var returnedContainer api.Container
					err := json.NewDecoder(getResponse.Body).Decode(&returnedContainer)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(returnedContainer).Should(Equal(api.Container{
						ExecutorGuid: "executor-guid-123",
						Guid:         reservedContainer.Guid,
						MemoryMB:     64,
						DiskMB:       512,
						CpuPercent:   0.5,
						State:        "reserved",
					}))
				})

				It("reduces the capacity by the amount reserved", func() {
					Ω(registry.CurrentCapacity()).Should(Equal(Registry.Capacity{
						MemoryMB:   960,
						DiskMB:     512,
						Containers: 1023,
					}))
				})
			})

			Describe("POST /containers/:guid/initialize", func() {
				var createResponse *http.Response

				JustBeforeEach(func() {
					createResponse = DoRequest(generator.RequestForHandler(
						api.InitializeContainer,
						router.Params{"guid": reservedContainer.Guid},
						nil,
					))
				})

				Context("when the container can be created", func() {
					BeforeEach(func() {
						wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
							return "some-handle", nil
						}
					})

					It("returns 201", func() {
						Ω(reserveResponse.StatusCode).Should(Equal(http.StatusCreated))
					})

					It("creates it with the configured owner", func() {
						created := wardenClient.Connection.Created()
						Ω(created).Should(HaveLen(1))

						Ω(created[0].Properties["owner"]).Should(Equal("some-container-owner-name"))
					})

					It("applies the memory, disk, and cpu limits", func() {
						limitedMemory := wardenClient.Connection.LimitedMemory("some-handle")
						Ω(limitedMemory).Should(HaveLen(1))

						limitedDisk := wardenClient.Connection.LimitedDisk("some-handle")
						Ω(limitedDisk).Should(HaveLen(1))

						limitedCPU := wardenClient.Connection.LimitedCPU("some-handle")
						Ω(limitedCPU).Should(HaveLen(1))

						Ω(limitedMemory[0].LimitInBytes).Should(Equal(uint64(64 * 1024 * 1024)))
						Ω(limitedDisk[0].ByteLimit).Should(Equal(uint64(512 * 1024 * 1024)))
						Ω(limitedCPU[0].LimitInShares).Should(Equal(uint64(512)))
					})

					Context("when a zero-value memory limit is specified", func() {
						BeforeEach(func() {
							reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
								MemoryMB:   0,
								DiskMB:     512,
								CpuPercent: 0.5,
							})
						})

						It("does not apply it", func() {
							limitedMemory := wardenClient.Connection.LimitedMemory("some-handle")
							Ω(limitedMemory).Should(BeEmpty())
						})
					})

					Context("when a zero-value disk limit is specified", func() {
						BeforeEach(func() {
							reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
								MemoryMB:   64,
								DiskMB:     0,
								CpuPercent: 0.5,
							})
						})

						It("does not apply it", func() {
							limitedDisk := wardenClient.Connection.LimitedDisk("some-handle")
							Ω(limitedDisk).Should(BeEmpty())
						})
					})

					Context("when a zero-value CPU percentage is specified", func() {
						BeforeEach(func() {
							reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
								MemoryMB:   64,
								DiskMB:     512,
								CpuPercent: 0,
							})
						})

						It("does not apply it", func() {
							limitedCPU := wardenClient.Connection.LimitedCPU("some-handle")
							Ω(limitedCPU).Should(BeEmpty())
						})
					})

					Context("when limiting memory fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.WhenLimitingMemory = func(string, warden.MemoryLimits) (warden.MemoryLimits, error) {
								return warden.MemoryLimits{}, disaster
							}
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})

					Context("when limiting disk fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.WhenLimitingDisk = func(string, warden.DiskLimits) (warden.DiskLimits, error) {
								return warden.DiskLimits{}, disaster
							}
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})

					Context("when limiting CPU fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.WhenLimitingCPU = func(string, warden.CPULimits) (warden.CPULimits, error) {
								return warden.CPULimits{}, disaster
							}
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})
				})

				Context("when for some reason the container fails to create", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
							return "", disaster
						}
					})

					It("returns 500", func() {
						Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
					})
				})

			})
		})

		Context("when the container cannot be reserved", func() {
			BeforeEach(func() {
				_, err := registry.Reserve(api.ContainerAllocationRequest{
					MemoryMB: 680,
					DiskMB:   680,
				})
				Ω(err).ShouldNot(HaveOccurred())

				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: 50.0,
				})
			})

			It("returns 503", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusServiceUnavailable))
			})
		})
	})

	Describe("POST /containers/:guid/run", func() {
		var runRequestBody io.Reader
		var runResponse *http.Response

		var containerGuid string

		BeforeEach(func() {
			runRequestBody = nil
			runResponse = nil

			allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
				MemoryMB:   64,
				DiskMB:     512,
				CpuPercent: 0.5,
			})

			allocResponse := DoRequest(generator.RequestForHandler(
				api.AllocateContainer,
				nil,
				allocRequestBody,
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}

			wardenClient.Connection.WhenListing = func(warden.Properties) ([]string, error) {
				return []string{"some-handle"}, nil
			}

			allocatedContainer := api.Container{}
			err := json.NewDecoder(allocResponse.Body).Decode(&allocatedContainer)
			Ω(err).ShouldNot(HaveOccurred())

			containerGuid = allocatedContainer.Guid

			initResponse := DoRequest(generator.RequestForHandler(
				api.InitializeContainer,
				router.Params{"guid": allocatedContainer.Guid},
				nil,
			))
			Ω(initResponse.StatusCode).Should(Equal(http.StatusCreated))
		})

		JustBeforeEach(func() {
			runResponse = DoRequest(generator.RequestForHandler(
				api.RunActions,
				router.Params{"guid": containerGuid},
				runRequestBody,
			))
		})

		Context("with a set of actions as the body", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
					successfulStream := make(chan warden.ProcessStream, 1)

					exitStatus := uint32(0)
					successfulStream <- warden.ProcessStream{ExitStatus: &exitStatus}

					return 0, successfulStream, nil
				}

				runRequestBody = MarshalledPayload(api.ContainerRunRequest{
					Actions: []models.ExecutorAction{
						{
							models.RunAction{
								Script: "ls -al",
							},
						},
					},
				})
			})

			It("returns 201", func() {
				Ω(runResponse.StatusCode).Should(Equal(http.StatusCreated))
				time.Sleep(time.Second)
			})

			It("performs the transformed actions", func() {
				spawned := []warden.ProcessSpec{}

				Eventually(func() []warden.ProcessSpec {
					spawned = wardenClient.Connection.SpawnedProcesses("some-handle")
					return spawned
				}).Should(HaveLen(1))

				Ω(spawned[0].Script).Should(Equal("ls -al"))
			})

			Context("when there is a completeURL and metadata", func() {
				var callbackHandler *ghttp.Server

				BeforeEach(func() {
					callbackHandler = ghttp.NewServer()

					runRequestBody = MarshalledPayload(api.ContainerRunRequest{
						CompleteURL: callbackHandler.URL() + "/result",
						Metadata:    []byte("some metadata"),
						Actions: []models.ExecutorAction{
							{
								models.RunAction{
									Script: "ls -al",
								},
							},
						},
					})
				})

				AfterEach(func() {
					callbackHandler.Close()
				})

				Context("and the completeURL succeeds", func() {
					BeforeEach(func() {
						callbackHandler.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("PUT", "/result"),
								ghttp.VerifyJSONRepresenting(api.ContainerRunResult{
									Metadata:      []byte("some metadata"),
									Failed:        false,
									FailureReason: "",
									Result:        "",
								}),
							),
						)
					})

					It("invokes the callback with failed false", func() {
						Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
					})
				})

				Context("and the completeURL fails", func() {
					BeforeEach(func() {
						callbackHandler.AppendHandlers(
							http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								callbackHandler.HTTPTestServer.CloseClientConnections()
							}),
							ghttp.RespondWith(http.StatusInternalServerError, ""),
							ghttp.RespondWith(http.StatusOK, ""),
						)
					})

					It("invokes the callback repeatedly", func() {
						Eventually(callbackHandler.ReceivedRequests, 5).Should(HaveLen(3))
					})
				})
			})

			Context("when the actions fail", func() {
				disaster := errors.New("because i said so")

				BeforeEach(func() {
					wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
						return 0, nil, disaster
					}
				})

				Context("and there is a completeURL", func() {
					var callbackHandler *ghttp.Server

					BeforeEach(func() {
						callbackHandler = ghttp.NewServer()

						callbackHandler.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("PUT", "/result"),
								ghttp.VerifyJSONRepresenting(api.ContainerRunResult{
									Failed:        true,
									FailureReason: "because i said so",
									Result:        "",
								}),
							),
						)

						runRequestBody = MarshalledPayload(api.ContainerRunRequest{
							CompleteURL: callbackHandler.URL() + "/result",
							Actions: []models.ExecutorAction{
								{
									models.RunAction{
										Script: "ls -al",
									},
								},
							},
						})
					})

					AfterEach(func() {
						callbackHandler.Close()
					})

					It("invokes the callback with failed true and a reason", func() {
						Eventually(callbackHandler.ReceivedRequests).Should(HaveLen(1))
					})
				})
			})
		})

		Context("with an invalid body", func() {
			BeforeEach(func() {
				runRequestBody = MarshalledPayload("lol")
			})

			It("returns 400", func() {
				Ω(runResponse.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})
	})

	Describe("DELETE /containers/:guid", func() {
		var deleteResponse *http.Response
		var containerGuid string

		JustBeforeEach(func() {
			deleteResponse = DoRequest(generator.RequestForHandler(
				api.DeleteContainer,
				router.Params{"guid": containerGuid},
				nil,
			))
		})

		Context("when the container has been allocated", func() {
			BeforeEach(func() {
				allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: 0.5,
				})

				allocResponse := DoRequest(generator.RequestForHandler(
					api.AllocateContainer,
					nil,
					allocRequestBody,
				))
				Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

				allocatedContainer := api.Container{}
				err := json.NewDecoder(allocResponse.Body).Decode(&allocatedContainer)
				Ω(err).ShouldNot(HaveOccurred())

				containerGuid = allocatedContainer.Guid
			})

			It("returns 200 OK", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("the previously allocated resources become available", func() {
				Ω(registry.CurrentCapacity()).Should(Equal(Registry.Capacity{
					MemoryMB:   1024,
					DiskMB:     1024,
					Containers: 1024,
				}))
			})
		})

		Context("when the container has been initalized", func() {
			BeforeEach(func() {
				allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB:   64,
					DiskMB:     512,
					CpuPercent: 0.5,
				})

				allocResponse := DoRequest(generator.RequestForHandler(
					api.AllocateContainer,
					nil,
					allocRequestBody,
				))
				Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

				wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
					return "some-handle", nil
				}

				wardenClient.Connection.WhenListing = func(warden.Properties) ([]string, error) {
					return []string{"some-handle"}, nil
				}

				allocatedContainer := api.Container{}
				err := json.NewDecoder(allocResponse.Body).Decode(&allocatedContainer)
				Ω(err).ShouldNot(HaveOccurred())

				containerGuid = allocatedContainer.Guid

				initResponse := DoRequest(generator.RequestForHandler(
					api.InitializeContainer,
					router.Params{"guid": allocatedContainer.Guid},
					nil,
				))
				Ω(initResponse.StatusCode).Should(Equal(http.StatusCreated))
			})

			It("returns 200 OK", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("destroys the warden container", func() {
				Ω(wardenClient.Connection.Destroyed()).Should(ContainElement("some-handle"))
			})

		})

	})
})
