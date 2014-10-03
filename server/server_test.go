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
	"github.com/cloudfoundry-incubator/executor/api/fakes"
	. "github.com/cloudfoundry-incubator/executor/server"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/runtime-schema/models"

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
	var depotClient *fakes.FakeClient
	var containerGuid string

	var server ifrit.Process
	var generator *rata.RequestGenerator

	var i = 0

	BeforeEach(func() {
		containerGuid = "container-guid"

		logger := lagertest.NewTestLogger("test")

		address := fmt.Sprintf("127.0.0.1:%d", 3150+i+(config.GinkgoConfig.ParallelNode*100))
		i++

		depotClient = new(fakes.FakeClient)

		server = ifrit.Envoke(&Server{
			Address:     address,
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

	Describe("GET /containers/:guid", func() {
		var getResponse *http.Response

		JustBeforeEach(func() {
			getResponse = DoRequest(generator.CreateRequest(
				api.GetContainer,
				rata.Params{"guid": containerGuid},
				nil,
			))
		})

		Context("when the container exists", func() {
			var expectedContainer api.Container

			BeforeEach(func() {
				expectedContainer = api.Container{
					Guid:     containerGuid,
					MemoryMB: 123,
					DiskMB:   456,
				}
				depotClient.GetContainerReturns(expectedContainer, nil)
			})

			It("returns 200 OK", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the correct container", func() {
				container := api.Container{}

				err := json.NewDecoder(getResponse.Body).Decode(&container)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(container).Should(Equal(expectedContainer))
			})
		})

		Context("when the container does not exist", func() {
			BeforeEach(func() {
				depotClient.GetContainerReturns(api.Container{}, api.ErrContainerNotFound)
			})

			It("returns 404 Not Found", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusNotFound))
			})
		})

		Context("when the container's existence cannot be determined", func() {
			BeforeEach(func() {
				depotClient.GetContainerReturns(api.Container{}, errors.New("KaBoom"))
			})

			It("returns 500 Internal Error", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("POST /containers/:guid", func() {
		var reserveRequestBody io.Reader
		var reserveResponse *http.Response

		BeforeEach(func() {
			reserveRequestBody = MarshalledPayload(api.ContainerAllocationRequest{
				MemoryMB: 64,
				DiskMB:   512,
			})

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
			var expectedContainer api.Container

			BeforeEach(func() {
				expectedContainer = api.Container{
					Guid:        containerGuid,
					MemoryMB:    64,
					DiskMB:      512,
					State:       "reserved",
					AllocatedAt: time.Now().UnixNano(),
				}

				depotClient.AllocateContainerReturns(expectedContainer, nil)
			})

			JustBeforeEach(func() {
				err := json.NewDecoder(reserveResponse.Body).Decode(&reservedContainer)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns 201", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusCreated))
			})

			It("returns a container", func() {
				Ω(reservedContainer).Should(Equal(expectedContainer))
			})
		})

		Context("when the container cannot be reserved because the guid is already taken", func() {
			BeforeEach(func() {
				depotClient.AllocateContainerReturns(api.Container{}, api.ErrContainerGuidNotAvailable)
			})

			It("returns 400", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when the container cannot be reserved because there is no room", func() {
			BeforeEach(func() {
				depotClient.AllocateContainerReturns(api.Container{}, api.ErrInsufficientResourcesAvailable)
			})

			It("returns 503", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusServiceUnavailable))
			})
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
				Ω(createResponse.StatusCode).Should(Equal(http.StatusCreated))
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
					depotClient.InitializeContainerReturns(api.Container{}, api.ErrLimitsInvalid)
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
			var expectedEnv []api.EnvironmentVariable

			var runRequest api.ContainerRunRequest

			BeforeEach(func() {
				expectedActions = []models.ExecutorAction{
					{
						models.RunAction{
							Path: "ls",
							Args: []string{"-al"},
						},
					},
				}

				expectedEnv = []api.EnvironmentVariable{
					{Name: "ENV1", Value: "val1"},
					{Name: "ENV2", Value: "val2"},
				}

				runRequest = api.ContainerRunRequest{
					Actions:     expectedActions,
					Env:         expectedEnv,
					CompleteURL: "http://example.com",
				}

				runRequestBody = MarshalledPayload(runRequest)
			})

			It("returns 201", func() {
				Ω(runResponse.StatusCode).Should(Equal(http.StatusCreated))
				time.Sleep(time.Second)
			})

			It("runs the actions", func() {
				Eventually(depotClient.RunCallCount).Should(Equal(1))

				guid, req := depotClient.RunArgsForCall(0)
				Ω(guid).Should(Equal(containerGuid))
				Ω(req).Should(Equal(runRequest))
			})

			Context("when the actions are invalid", func() {
				BeforeEach(func() {
					depotClient.RunStub = func(guid string, runRequest api.ContainerRunRequest) error {
						return api.ErrStepsInvalid
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
		var expectedContainers []api.Container

		Context("when we can succesfully get containers ", func() {
			var listResponse *http.Response

			BeforeEach(func() {
				expectedContainers = []api.Container{
					api.Container{Guid: "first-container"},
					api.Container{Guid: "second-container"},
				}

				depotClient.ListContainersReturns(expectedContainers, nil)

				listResponse = DoRequest(generator.CreateRequest(
					api.ListContainers,
					nil,
					nil,
				))
			})

			It("returns 200", func() {
				Ω(listResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("should return all reserved containers", func() {
				containers := []api.Container{}
				err := json.NewDecoder(listResponse.Body).Decode(&containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(containers).Should(Equal(expectedContainers))
			})
		})

		Context("when we cannot get containers", func() {
			var listResponse *http.Response

			BeforeEach(func() {
				depotClient.ListContainersReturns([]api.Container{}, errors.New("ahh!"))

				listResponse = DoRequest(generator.CreateRequest(
					api.ListContainers,
					nil,
					nil,
				))
			})

			It("returns 500", func() {
				Ω(listResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})
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
				depotClient.DeleteContainerReturns(api.ErrContainerNotFound)
			})

			It("returns a 404", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusNotFound))
			})
		})
	})

	Describe("GET /resources/remaining", func() {
		var resourcesResponse *http.Response

		JustBeforeEach(func() {
			resourcesResponse = DoRequest(generator.CreateRequest(
				api.GetRemainingResources,
				nil,
				nil,
			))
		})

		Context("when we can determine remaining resources", func() {
			var expectedExecutorResources api.ExecutorResources

			BeforeEach(func() {
				expectedExecutorResources = api.ExecutorResources{
					MemoryMB:   512,
					DiskMB:     128,
					Containers: 10,
				}
				depotClient.RemainingResourcesReturns(expectedExecutorResources, nil)
			})

			It("returns 200", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("should return the remaining resources", func() {
				var resources api.ExecutorResources
				err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))
				Ω(resources).Should(Equal(expectedExecutorResources))
			})
		})

		Context("when we cannot determine remaining resources", func() {
			BeforeEach(func() {
				depotClient.RemainingResourcesReturns(api.ExecutorResources{}, errors.New("BOOM"))
			})

			It("returns 500", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GET /resources/total", func() {
		var resourcesResponse *http.Response
		var expectedTotalResources api.ExecutorResources

		JustBeforeEach(func() {
			resourcesResponse = DoRequest(generator.CreateRequest(
				api.GetTotalResources,
				nil,
				nil,
			))
		})

		Context("when total resources are available", func() {
			BeforeEach(func() {
				expectedTotalResources = api.ExecutorResources{
					MemoryMB:   64,
					DiskMB:     512,
					Containers: 10,
				}
				depotClient.TotalResourcesReturns(expectedTotalResources, nil)
			})

			It("returns 200 ok", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the total resources", func() {
				var resources api.ExecutorResources
				err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(resources).Should(Equal(expectedTotalResources))
			})
		})

		Context("when we cannot determine the total resources", func() {
			BeforeEach(func() {
				depotClient.TotalResourcesReturns(api.ExecutorResources{}, errors.New("BOOM"))
			})

			It("returns 500", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("GET /ping", func() {
		Context("when Garden responds to ping", func() {
			BeforeEach(func() {
				depotClient.PingReturns(nil)
			})

			It("should 200", func() {
				response := DoRequest(generator.CreateRequest(api.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})
		})

		Context("when Garden returns an error", func() {
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
