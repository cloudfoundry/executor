package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/executor"
	Registry "github.com/cloudfoundry-incubator/executor/registry"
	. "github.com/cloudfoundry-incubator/executor/server"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
	"github.com/pivotal-golang/cacheddownloader/fakecacheddownloader"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/rata"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
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
	var containerGuid string

	var server ifrit.Process
	var generator *rata.RequestGenerator
	var timeProvider *faketimeprovider.FakeTimeProvider

	var i = 0

	BeforeEach(func() {
		containerGuid = "container-guid"

		wardenClient = fake_warden_client.New()

		timeProvider = faketimeprovider.New(time.Now())
		registry = Registry.New(Registry.Capacity{
			MemoryMB:   1024,
			DiskMB:     1024,
			Containers: 1024,
		}, timeProvider)

		gosteno.EnterTestMode(gosteno.LOG_DEBUG)
		logger := gosteno.NewLogger("api-test-logger")
		logEmitter, err := emitter.NewEmitter("", "", "", "", nil)
		Ω(err).ShouldNot(HaveOccurred())

		address := fmt.Sprintf("127.0.0.1:%d", 3150+i+(config.GinkgoConfig.ParallelNode*100))
		i++

		runWaitGroup := new(sync.WaitGroup)
		runCanceller := make(chan struct{})
		depotClient := executor.NewClient(runWaitGroup, runCanceller, wardenClient, registry, logger)

		server = ifrit.Envoke(&Server{
			Transformer: transformer.NewTransformer(
				logEmitter,
				fakecacheddownloader.New(),
				new(fake_uploader.FakeUploader),
				&fake_extractor.FakeExtractor{},
				&fake_compressor.FakeCompressor{},
				logger,
				"/tmp",
			),
			Address:               address,
			Registry:              registry,
			WardenClient:          wardenClient,
			ContainerOwnerName:    "some-container-owner-name",
			ContainerMaxCPUShares: 1024,
			Logger:                logger,
			DepotClient:           depotClient,
		})

		generator = rata.NewRequestGenerator("http://"+address, api.Routes)
	})

	AfterEach(func() {
		server.Signal(os.Kill)
		Eventually(server.Wait(), 3).Should(Receive())
	})

	DoRequest := func(req *http.Request, err error) *http.Response {
		Ω(err).ShouldNot(HaveOccurred())
		client := http.Client{}
		resp, err := client.Do(req)
		Ω(err).ShouldNot(HaveOccurred())

		return resp
	}

	Describe("POST /containers/:guid", func() {
		var reserveRequestBody io.Reader
		var reserveResponse *http.Response

		BeforeEach(func() {
			reserveRequestBody = nil
			reserveResponse = nil
		})

		JustBeforeEach(func() {
			reserveResponse = DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": containerGuid},
				reserveRequestBody,
			))
		})

		Context("when there are containers available", func() {
			var reservedContainer api.Container

			BeforeEach(func() {
				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   512,
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
					Guid:        containerGuid,
					MemoryMB:    64,
					DiskMB:      512,
					State:       "reserved",
					AllocatedAt: timeProvider.Time().UnixNano(),
				}))
			})

			Context("and we request an available container", func() {
				var getResponse *http.Response

				JustBeforeEach(func() {
					getResponse = DoRequest(generator.CreateRequest(
						api.GetContainer,
						rata.Params{"guid": containerGuid},
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
						Guid:        containerGuid,
						MemoryMB:    64,
						DiskMB:      512,
						State:       "reserved",
						AllocatedAt: timeProvider.Time().UnixNano(),
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
				var createRequestBody io.Reader

				JustBeforeEach(func() {
					createResponse = DoRequest(generator.CreateRequest(
						api.InitializeContainer,
						rata.Params{"guid": containerGuid},
						createRequestBody,
					))
				})

				BeforeEach(func() {
					createRequestBody = MarshalledPayload(api.ContainerInitializationRequest{
						CpuPercent: 50.0,
					})
				})

				Context("when the requested CPU percent is > 100", func() {
					BeforeEach(func() {
						createRequestBody = MarshalledPayload(api.ContainerInitializationRequest{
							CpuPercent: 101.0,
						})
					})

					It("returns 400", func() {
						Ω(createResponse.StatusCode).Should(Equal(http.StatusBadRequest))
					})
				})

				Context("when the requested CPU percent is < 0", func() {
					BeforeEach(func() {
						createRequestBody = MarshalledPayload(api.ContainerInitializationRequest{
							CpuPercent: -14.0,
						})
					})

					It("returns 400", func() {
						Ω(createResponse.StatusCode).Should(Equal(http.StatusBadRequest))
					})
				})

				Context("when the container can be created", func() {
					BeforeEach(func() {
						wardenClient.Connection.CreateReturns("some-handle", nil)
					})

					It("returns 201", func() {
						Ω(reserveResponse.StatusCode).Should(Equal(http.StatusCreated))
					})

					It("creates it with the configured owner", func() {
						created := wardenClient.Connection.CreateArgsForCall(0)
						Ω(created.Properties["owner"]).Should(Equal("some-container-owner-name"))
					})

					It("applies the memory, disk, and cpu limits", func() {
						handle, limitedMemory := wardenClient.Connection.LimitMemoryArgsForCall(0)
						Ω(handle).Should(Equal("some-handle"))
						Ω(limitedMemory.LimitInBytes).Should(Equal(uint64(64 * 1024 * 1024)))

						handle, limitedDisk := wardenClient.Connection.LimitDiskArgsForCall(0)
						Ω(handle).Should(Equal("some-handle"))
						Ω(limitedDisk.ByteHard).Should(Equal(uint64(512 * 1024 * 1024)))

						handle, limitedCPU := wardenClient.Connection.LimitCPUArgsForCall(0)
						Ω(handle).Should(Equal("some-handle"))
						Ω(limitedCPU.LimitInShares).Should(Equal(uint64(512)))
					})

					Context("when ports are exposed", func() {
						BeforeEach(func() {
							createRequestBody = MarshalledPayload(api.ContainerInitializationRequest{
								CpuPercent: 0.5,
								Ports: []api.PortMapping{
									{ContainerPort: 8080, HostPort: 0},
									{ContainerPort: 8081, HostPort: 1234},
								},
							})
						})

						It("exposes the configured ports", func() {
							handle, netInH, netInC := wardenClient.Connection.NetInArgsForCall(0)
							Ω(handle).Should(Equal("some-handle"))
							Ω(netInH).Should(Equal(uint32(0)))
							Ω(netInC).Should(Equal(uint32(8080)))

							handle, netInH, netInC = wardenClient.Connection.NetInArgsForCall(1)
							Ω(handle).Should(Equal("some-handle"))
							Ω(netInH).Should(Equal(uint32(1234)))
							Ω(netInC).Should(Equal(uint32(8081)))
						})

						Context("when net-in succeeds", func() {
							BeforeEach(func() {
								calls := uint32(0)
								wardenClient.Connection.NetInStub = func(string, uint32, uint32) (uint32, uint32, error) {
									calls++
									return 1234 * calls, 4567 * calls, nil
								}
							})

							It("returns the mapped ports in the response", func() {
								var initialized api.Container

								err := json.NewDecoder(createResponse.Body).Decode(&initialized)
								Ω(err).ShouldNot(HaveOccurred())

								Ω(initialized.Ports).Should(Equal([]api.PortMapping{
									{HostPort: 1234, ContainerPort: 4567},
									{HostPort: 2468, ContainerPort: 9134},
								}))
							})
						})

						Context("when mapping the ports fails", func() {
							disaster := errors.New("oh no!")

							BeforeEach(func() {
								wardenClient.Connection.NetInReturns(0, 0, disaster)
							})

							It("returns 500", func() {
								Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
							})
						})
					})

					Context("when a zero-value memory limit is specified", func() {
						BeforeEach(func() {
							reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
								MemoryMB: 0,
								DiskMB:   512,
							})
						})

						It("does not apply it", func() {
							Ω(wardenClient.Connection.LimitMemoryCallCount()).Should(BeZero())
						})
					})

					Context("when a zero-value disk limit is specified", func() {
						BeforeEach(func() {
							reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
								MemoryMB: 64,
								DiskMB:   0,
							})
						})

						It("does not apply it", func() {
							Ω(wardenClient.Connection.LimitDiskCallCount()).Should(BeZero())
						})
					})

					Context("when a zero-value CPU percentage is specified", func() {
						BeforeEach(func() {
							createRequestBody = MarshalledPayload(api.ContainerInitializationRequest{
								CpuPercent: 0,
							})
						})

						It("does not apply it", func() {
							Ω(wardenClient.Connection.LimitCPUCallCount()).Should(BeZero())
						})
					})

					Context("when limiting memory fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.LimitMemoryReturns(warden.MemoryLimits{}, disaster)
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})

					Context("when limiting disk fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.LimitDiskReturns(warden.DiskLimits{}, disaster)
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})

					Context("when limiting CPU fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							wardenClient.Connection.LimitCPUReturns(warden.CPULimits{}, disaster)
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
					})
				})

				Context("when for some reason the container fails to create", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						wardenClient.Connection.CreateReturns("", disaster)
					})

					It("returns 500", func() {
						Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
					})
				})
			})
		})

		Context("when the container cannot be reserved because the guid is already taken", func() {
			BeforeEach(func() {
				_, err := registry.Reserve(containerGuid, api.ContainerAllocationRequest{
					MemoryMB: 1,
					DiskMB:   1,
				})
				Ω(err).ShouldNot(HaveOccurred())

				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 1,
					DiskMB:   1,
				})
			})

			It("returns 400", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when the container cannot be reserved because there is no room", func() {
			BeforeEach(func() {
				_, err := registry.Reserve("another-container-guid", api.ContainerAllocationRequest{
					MemoryMB: 680,
					DiskMB:   680,
				})
				Ω(err).ShouldNot(HaveOccurred())

				reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   512,
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

		BeforeEach(func() {
			runRequestBody = nil
			runResponse = nil

			allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
				MemoryMB: 64,
				DiskMB:   512,
			})

			allocResponse := DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": containerGuid},
				allocRequestBody,
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

			wardenClient.Connection.CreateReturns("some-handle", nil)
			wardenClient.Connection.ListReturns([]string{"some-handle"}, nil)

			initResponse := DoRequest(generator.CreateRequest(
				api.InitializeContainer,
				rata.Params{"guid": containerGuid},
				MarshalledPayload(api.ContainerInitializationRequest{
					CpuPercent: 50.0,
				}),
			))
			Ω(initResponse.StatusCode).Should(Equal(http.StatusCreated))
		})

		JustBeforeEach(func() {
			runResponse = DoRequest(generator.CreateRequest(
				api.RunActions,
				rata.Params{"guid": containerGuid},
				runRequestBody,
			))
		})

		Context("with a set of actions as the body", func() {
			BeforeEach(func() {
				wardenClient.Connection.RunReturns(new(wfakes.FakeProcess), nil)

				runRequestBody = MarshalledPayload(api.ContainerRunRequest{
					Actions: []models.ExecutorAction{
						{
							models.RunAction{
								Path: "ls",
								Args: []string{"-al"},
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
				Eventually(wardenClient.Connection.RunCallCount).ShouldNot(BeZero())

				handle, spec, _ := wardenClient.Connection.RunArgsForCall(0)
				Ω(handle).Should(Equal("some-handle"))
				Ω(spec.Path).Should(Equal("ls"))
				Ω(spec.Args).Should(Equal([]string{"-al"}))
			})

			Context("when the actions are invalid", func() {
				BeforeEach(func() {
					runRequestBody = MarshalledPayload(api.ContainerRunRequest{
						Actions: []models.ExecutorAction{
							{
								models.MonitorAction{
									HealthyHook: models.HealthRequest{
										URL: "some/bogus/url",
									},
									Action: models.ExecutorAction{
										models.RunAction{
											Path: "ls",
											Args: []string{"-al"},
										},
									},
								},
							},
						},
					})
				})

				It("returns 400", func() {
					Ω(runResponse.StatusCode).Should(Equal(http.StatusBadRequest))
				})
			})

			Context("when there is a completeURL and metadata", func() {
				var callbackHandler *ghttp.Server

				BeforeEach(func() {
					callbackHandler = ghttp.NewServer()
					runRequestBody = MarshalledPayload(api.ContainerRunRequest{
						CompleteURL: callbackHandler.URL() + "/result",
						Actions: []models.ExecutorAction{
							{
								models.RunAction{
									Path: "ls",
									Args: []string{"-al"},
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
									Guid:          containerGuid,
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

					It("destroys the container and removes it from the registry", func() {
						Eventually(wardenClient.Connection.DestroyCallCount).Should(Equal(1))
						Ω(wardenClient.Connection.DestroyArgsForCall(0)).Should(Equal("some-handle"))
					})

					It("frees the container's reserved resources", func() {
						Eventually(registry.CurrentCapacity).Should(Equal(Registry.Capacity{
							MemoryMB:   1024,
							DiskMB:     1024,
							Containers: 1024,
						}))
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
					wardenClient.Connection.RunReturns(nil, disaster)
				})

				Context("and there is a completeURL", func() {
					var callbackHandler *ghttp.Server

					BeforeEach(func() {
						callbackHandler = ghttp.NewServer()

						callbackHandler.AppendHandlers(
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("PUT", "/result"),
								ghttp.VerifyJSONRepresenting(api.ContainerRunResult{
									Guid:          containerGuid,
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
										Path: "ls",
										Args: []string{"-al"},
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

	Describe("GET /containers", func() {
		BeforeEach(func() {
			allocResponse := DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": "first-container"},
				MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   512,
				}),
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

			allocResponse = DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": "second-container"},
				MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   512,
				}),
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))
		})

		It("should return all reserved containers", func() {
			listResponse := DoRequest(generator.CreateRequest(
				api.ListContainers,
				nil,
				nil,
			))

			containers := []api.Container{}
			err := json.NewDecoder(listResponse.Body).Decode(&containers)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(containers).Should(HaveLen(2))
			Ω([]string{containers[0].Guid, containers[1].Guid}).Should(ContainElement("first-container"))
			Ω([]string{containers[0].Guid, containers[1].Guid}).Should(ContainElement("second-container"))
		})
	})

	Describe("DELETE /containers/:guid", func() {
		var deleteResponse *http.Response

		JustBeforeEach(func() {
			deleteResponse = DoRequest(generator.CreateRequest(
				api.DeleteContainer,
				rata.Params{"guid": containerGuid},
				nil,
			))
		})

		Context("when the container has been allocated", func() {
			BeforeEach(func() {
				allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
					MemoryMB: 64,
					DiskMB:   512,
				})

				allocResponse := DoRequest(generator.CreateRequest(
					api.AllocateContainer,
					rata.Params{"guid": containerGuid},
					allocRequestBody,
				))
				Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))
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
					MemoryMB: 64,
					DiskMB:   512,
				})

				allocResponse := DoRequest(generator.CreateRequest(
					api.AllocateContainer,
					rata.Params{"guid": containerGuid},
					allocRequestBody,
				))
				Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

				wardenClient.Connection.CreateReturns("some-handle", nil)
				wardenClient.Connection.ListReturns([]string{"some-handle"}, nil)

				initResponse := DoRequest(generator.CreateRequest(
					api.InitializeContainer,
					rata.Params{"guid": containerGuid},
					MarshalledPayload(api.ContainerInitializationRequest{
						CpuPercent: 50.0,
					}),
				))
				Ω(initResponse.StatusCode).Should(Equal(http.StatusCreated))
			})

			It("returns 200 OK", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("destroys the warden container", func() {
				Eventually(wardenClient.Connection.DestroyCallCount).Should(Equal(1))
				Ω(wardenClient.Connection.DestroyArgsForCall(0)).Should(Equal("some-handle"))
			})

			Describe("when deleting the container fails", func() {
				BeforeEach(func() {
					wardenClient.Connection.DestroyReturns(errors.New("oh no!"))
				})

				It("returns a 500", func() {
					Ω(deleteResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
				})
			})
		})
	})

	Describe("GET /resources/remaining", func() {
		var resourcesResponse *http.Response

		BeforeEach(func() {
			resourcesResponse = nil

			allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
				MemoryMB: 64,
				DiskMB:   512,
			})

			allocResponse := DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": containerGuid},
				allocRequestBody,
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

			resourcesResponse = DoRequest(generator.CreateRequest(
				api.GetRemainingResources,
				nil,
				nil,
			))
		})

		It("should return the remaining resources", func() {
			var resources api.ExecutorResources
			err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))

			Ω(resources.MemoryMB).Should(Equal(1024 - 64))
			Ω(resources.DiskMB).Should(Equal(1024 - 512))
			Ω(resources.Containers).Should(Equal(1024 - 1))
		})
	})

	Describe("GET /resources/total", func() {
		var resourcesResponse *http.Response

		BeforeEach(func() {
			resourcesResponse = nil

			allocRequestBody := MarshalledPayload(api.ContainerAllocationRequest{
				MemoryMB: 64,
				DiskMB:   512,
			})

			allocResponse := DoRequest(generator.CreateRequest(
				api.AllocateContainer,
				rata.Params{"guid": containerGuid},
				allocRequestBody,
			))
			Ω(allocResponse.StatusCode).Should(Equal(http.StatusCreated))

			resourcesResponse = DoRequest(generator.CreateRequest(
				api.GetTotalResources,
				nil,
				nil,
			))
		})

		It("should return the total resources", func() {
			var resources api.ExecutorResources
			err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))

			Ω(resources.MemoryMB).Should(Equal(1024))
			Ω(resources.DiskMB).Should(Equal(1024))
			Ω(resources.Containers).Should(Equal(1024))
		})
	})

	Describe("GET /ping", func() {
		Context("when Warden responds to ping", func() {
			BeforeEach(func() {
				wardenClient.Connection.PingReturns(nil)
			})

			It("should 200", func() {
				response := DoRequest(generator.CreateRequest(api.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})
		})

		Context("when Warden returns an error", func() {
			BeforeEach(func() {
				wardenClient.Connection.PingReturns(errors.New("oh no!"))
			})

			It("should 502", func() {
				response := DoRequest(generator.CreateRequest(api.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusBadGateway))
			})
		})
	})
})
