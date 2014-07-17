package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/executor/fakes"
	Registry "github.com/cloudfoundry-incubator/executor/registry"
	. "github.com/cloudfoundry-incubator/executor/server"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/rata"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
)

func MarshalledPayload(payload interface{}) io.Reader {
	reqBody, err := json.Marshal(payload)
	Ω(err).ShouldNot(HaveOccurred())

	return bytes.NewBuffer(reqBody)
}

var _ = Describe("Api", func() {
	var registry Registry.Registry
	var wardenClient *fake_warden_client.FakeClient
	var depotClient *fakes.FakeClient
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

		address := fmt.Sprintf("127.0.0.1:%d", 3150+i+(config.GinkgoConfig.ParallelNode*100))
		i++

		depotClient = new(fakes.FakeClient)

		server = ifrit.Envoke(&Server{
			Address:     address,
			Registry:    registry,
			Logger:      logger,
			DepotClient: depotClient,
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
				Context("when the container can be created", func() {
					var expectedRequest api.ContainerInitializationRequest
					var createResponse *http.Response
					var expectedContainer api.Container

					BeforeEach(func() {
						expectedRequest = api.ContainerInitializationRequest{
							CpuPercent: 50.0,
							Ports: []api.PortMapping{
								{ContainerPort: 8080, HostPort: 0},
								{ContainerPort: 8081, HostPort: 1234},
							},
						}

						expectedContainer = api.Container{
							Ports: []api.PortMapping{
								{HostPort: 1234, ContainerPort: 4567},
								{HostPort: 2468, ContainerPort: 9134},
							},
						}
						depotClient.InitializeContainerReturns(expectedContainer, nil)

						createResponse = DoRequest(generator.CreateRequest(
							api.InitializeContainer,
							rata.Params{"guid": containerGuid},
							MarshalledPayload(expectedRequest),
						))
					})

					It("returns 201", func() {
						Ω(reserveResponse.StatusCode).Should(Equal(http.StatusCreated))
					})

					It("creates the container", func() {
						Ω(depotClient.InitializeContainerCallCount()).Should(Equal(1))
						guid, containerInitReq := depotClient.InitializeContainerArgsForCall(0)
						Ω(guid).Should(Equal(containerGuid))
						Ω(containerInitReq).Should(Equal(expectedRequest))
					})

					It("returns the serialized initialized container", func() {
						var initializedContainer api.Container

						err := json.NewDecoder(createResponse.Body).Decode(&initializedContainer)
						Ω(err).ShouldNot(HaveOccurred())
						Ω(initializedContainer).Should(Equal(expectedContainer))
					})
				})

				Context("when the request is invalid json", func() {
					var createResponse *http.Response
					BeforeEach(func() {
						createResponse = DoRequest(generator.CreateRequest(
							api.InitializeContainer,
							rata.Params{"guid": containerGuid},
							MarshalledPayload("asdasdasdasdasdadsads"),
						))
					})

					It("returns 400", func() {
						Ω(createResponse.StatusCode).Should(Equal(http.StatusBadRequest))
					})
				})

				Context("when the container cannot be created", func() {
					var createResponse *http.Response
					JustBeforeEach(func() {
						createResponse = DoRequest(generator.CreateRequest(
							api.InitializeContainer,
							rata.Params{"guid": containerGuid},
							MarshalledPayload(api.ContainerInitializationRequest{}),
						))
					})

					Context("when the requested limits are invalid", func() {
						BeforeEach(func() {
							depotClient.InitializeContainerReturns(api.Container{}, executor.LimitsInvalid)
						})

						It("returns 400", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusBadRequest))
						})
					})

					Context("when for some reason the container fails to create", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							depotClient.InitializeContainerReturns(api.Container{}, disaster)
						})

						It("returns 500", func() {
							Ω(createResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
						})
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
			var expectedActions []models.ExecutorAction

			BeforeEach(func() {
				expectedActions = []models.ExecutorAction{
					{
						models.RunAction{
							Path: "ls",
							Args: []string{"-al"},
						},
					},
				}
				runRequestBody = MarshalledPayload(api.ContainerRunRequest{
					Actions:     expectedActions,
					CompleteURL: "http://example.com",
				})
			})

			It("returns 201", func() {
				Ω(runResponse.StatusCode).Should(Equal(http.StatusCreated))
				time.Sleep(time.Second)
			})

			It("runs the actions", func() {
				Eventually(depotClient.RunContainerCallCount).Should(Equal(1))

				guid, actions, completeURL := depotClient.RunContainerArgsForCall(0)
				Ω(guid).Should(Equal(containerGuid))
				Ω(actions).Should(Equal(expectedActions))
				Ω(completeURL).Should(Equal("http://example.com"))
			})

			Context("when the actions are invalid", func() {
				BeforeEach(func() {
					depotClient.RunContainerStub = func(guid string, actions []models.ExecutorAction, completeURL string) error {
						return executor.StepsInvalid
					}

					runRequestBody = MarshalledPayload(api.ContainerRunRequest{
						Actions: []models.ExecutorAction{},
					})
				})

				It("returns 400", func() {
					Ω(runResponse.StatusCode).Should(Equal(http.StatusBadRequest))
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

		Context("when the container exists", func() {
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

			It("deletes the container", func() {
				Ω(depotClient.DeleteContainerCallCount()).Should(Equal(1))
				Ω(depotClient.DeleteContainerArgsForCall(0)).Should(Equal(containerGuid))
			})

			Context("when deleting the container fails", func() {
				BeforeEach(func() {
					depotClient.DeleteContainerReturns(errors.New("oh no!"))
				})

				It("returns a 500", func() {
					Ω(deleteResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
				})
			})
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				depotClient.DeleteContainerReturns(executor.ContainerNotFound)
			})

			It("returns a 404", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusNotFound))
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
				depotClient.PingReturns(nil)
			})

			It("should 200", func() {
				response := DoRequest(generator.CreateRequest(api.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})
		})

		Context("when Warden returns an error", func() {
			BeforeEach(func() {
				depotClient.PingReturns(errors.New("oh no!"))
			})

			It("should 502", func() {
				response := DoRequest(generator.CreateRequest(api.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusBadGateway))
			})
		})
	})
})
