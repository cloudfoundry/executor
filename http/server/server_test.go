package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/fakes"
	ehttp "github.com/cloudfoundry-incubator/executor/http"
	. "github.com/cloudfoundry-incubator/executor/http/server"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/vito/go-sse/sse"

	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/rata"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

func MarshalledPayload(payload interface{}) io.Reader {
	reqBody, err := json.Marshal(payload)
	Ω(err).ShouldNot(HaveOccurred())

	return bytes.NewBuffer(reqBody)
}

var _ = Describe("Api", func() {
	var depotClientProvider *fakes.FakeClientProvider
	var depotClient *fakes.FakeClient
	var containerGuid string

	var server ifrit.Process
	var generator *rata.RequestGenerator
	var logger *lagertest.TestLogger

	var i = 0

	action := &models.RunAction{
		Path: "ls",
	}

	BeforeEach(func() {
		containerGuid = "container-guid"

		logger = lagertest.NewTestLogger("test")

		address := fmt.Sprintf("127.0.0.1:%d", 13150+i+(config.GinkgoConfig.ParallelNode*100))
		i++

		depotClientProvider = new(fakes.FakeClientProvider)
		depotClient = new(fakes.FakeClient)
		depotClientProvider.WithLoggerReturns(depotClient)

		server = ginkgomon.Invoke(&Server{
			Address:             address,
			Logger:              logger,
			DepotClientProvider: depotClientProvider,
		})

		generator = rata.NewRequestGenerator("http://"+address, ehttp.Routes)
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
				ehttp.GetContainer,
				rata.Params{"guid": containerGuid},
				nil,
			))
		})

		Context("when the container exists", func() {
			var expectedContainer executor.Container

			BeforeEach(func() {
				expectedContainer = executor.Container{
					Guid:     containerGuid,
					Action:   action,
					MemoryMB: 123,
					DiskMB:   456,
				}
				depotClient.GetContainerReturns(expectedContainer, nil)
			})

			It("returns 200 OK", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the correct container", func() {
				container := executor.Container{}

				err := json.NewDecoder(getResponse.Body).Decode(&container)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(container).Should(Equal(expectedContainer))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})
		})

		Context("when the container does not exist", func() {
			BeforeEach(func() {
				depotClient.GetContainerReturns(executor.Container{}, executor.ErrContainerNotFound)
			})

			It("returns 404 Not Found", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusNotFound))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-handler.failed-to-get-container",
					"test.request.done",
				}))
			})
		})

		Context("when the container's existence cannot be determined", func() {
			BeforeEach(func() {
				depotClient.GetContainerReturns(executor.Container{}, errors.New("KaBoom"))
			})

			It("returns 500 Internal Error", func() {
				Ω(getResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-handler.failed-to-get-container",
					"test.request.done",
				}))
			})
		})
	})

	Describe("POST /containers", func() {
		var reserveRequestBody io.Reader
		var reserveResponse *http.Response

		BeforeEach(func() {
			reserveRequestBody = MarshalledPayload([]executor.Container{
				{
					Action:    action,
					MemoryMB:  64,
					DiskMB:    512,
					CPUWeight: 50,
					Ports: []executor.PortMapping{
						{ContainerPort: 8080, HostPort: 0},
						{ContainerPort: 8081, HostPort: 1234},
					},
				}})

			reserveResponse = nil
		})

		JustBeforeEach(func() {
			reserveResponse = DoRequest(generator.CreateRequest(
				ehttp.AllocateContainers,
				rata.Params{"guid": containerGuid},
				reserveRequestBody,
			))

		})

		Context("when there are containers available", func() {
			var errMessageMap map[string]string

			JustBeforeEach(func() {
				err := json.NewDecoder(reserveResponse.Body).Decode(&errMessageMap)
				Ω(err).ShouldNot(HaveOccurred())
			})

			BeforeEach(func() {
				depotClient.AllocateContainersReturns(map[string]string{}, nil)
				errMessageMap = map[string]string{}
			})

			It("returns 200", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns empty error map", func() {
				Ω(errMessageMap).Should(BeEmpty())
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})
		})

		Context("when the container cannot be reserved because the guid is already taken", func() {
			var errMessageMap map[string]string

			JustBeforeEach(func() {
				err := json.NewDecoder(reserveResponse.Body).Decode(&errMessageMap)
				Ω(err).ShouldNot(HaveOccurred())
			})
			BeforeEach(func() {
				depotClient.AllocateContainersReturns(map[string]string{
					containerGuid: executor.ErrContainerGuidNotAvailable.Error(),
				}, nil)
			})

			It("returns 200", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns error map with appropriate error message", func() {
				Ω(errMessageMap[containerGuid]).Should(Equal(executor.ErrContainerGuidNotAvailable.Error()))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.allocate-handler.failed-to-allocate-containers",
					"test.request.done",
				}))
			})
		})

		Context("when the container cannot be reserved because there is no room", func() {
			var errMessageMap map[string]string

			JustBeforeEach(func() {
				err := json.NewDecoder(reserveResponse.Body).Decode(&errMessageMap)
				Ω(err).ShouldNot(HaveOccurred())
			})
			BeforeEach(func() {
				depotClient.AllocateContainersReturns(map[string]string{
					containerGuid: executor.ErrInsufficientResourcesAvailable.Error(),
				}, nil)
			})

			It("returns 200", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns error map with appropriate error message", func() {
				Ω(errMessageMap[containerGuid]).Should(Equal(executor.ErrInsufficientResourcesAvailable.Error()))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.allocate-handler.failed-to-allocate-containers",
					"test.request.done",
				}))
			})
		})

		Context("when the container cannot be reserved because depot client returns an error", func() {
			BeforeEach(func() {
				depotClient.AllocateContainersReturns(map[string]string{}, executor.ErrFailureToCheckSpace)
			})

			It("returns 500", func() {
				Ω(reserveResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.allocate-handler.failed-to-allocate-containers",
					"test.request.done",
				}))
			})
		})
	})

	Describe("POST /containers/:guid/run", func() {
		var runRequestBody io.Reader
		var runResponse *http.Response

		var expectedAction models.Action
		var expectedEnv []executor.EnvironmentVariable

		BeforeEach(func() {
			runRequestBody = nil
			runResponse = nil

			expectedAction = &models.RunAction{
				Path: "ls",
				Args: []string{"-al"},
			}

			expectedEnv = []executor.EnvironmentVariable{
				{Name: "ENV1", Value: "val1"},
				{Name: "ENV2", Value: "val2"},
			}
		})

		JustBeforeEach(func() {
			runResponse = DoRequest(generator.CreateRequest(
				ehttp.RunContainer,
				rata.Params{"guid": containerGuid},
				nil,
			))
		})

		It("returns 201", func() {
			Ω(runResponse.StatusCode).Should(Equal(http.StatusCreated))
			time.Sleep(time.Second)
		})

		It("runs the actions", func() {
			Eventually(depotClient.RunContainerCallCount).Should(Equal(1))

			guid := depotClient.RunContainerArgsForCall(0)
			Ω(guid).Should(Equal(containerGuid))
		})

		It("logs the request", func() {
			Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
				"test.request.serving",
				"test.request.done",
			}))
		})

		Context("when the run action fails", func() {
			BeforeEach(func() {
				depotClient.RunContainerReturns(executor.ErrContainerNotFound)
			})

			It("returns 404", func() {
				Ω(runResponse.StatusCode).Should(Equal(http.StatusNotFound))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.run-handler.run-actions-failed",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /containers", func() {
		var expectedContainers []executor.Container

		Context("when we can succesfully get containers", func() {
			var (
				listRequest  *http.Request
				listResponse *http.Response
			)

			BeforeEach(func() {
				expectedContainers = []executor.Container{
					executor.Container{Guid: "first-container", Action: action},
					executor.Container{Guid: "second-container", Action: action},
				}

				depotClient.ListContainersReturns(expectedContainers, nil)

				var err error
				listRequest, err = generator.CreateRequest(
					ehttp.ListContainers,
					nil,
					nil,
				)
				Ω(err).ShouldNot(HaveOccurred())
			})

			JustBeforeEach(func() {
				listResponse = DoRequest(listRequest, nil)
			})

			It("returns 200", func() {
				Ω(listResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("should return all reserved containers", func() {
				containers := []executor.Container{}
				err := json.NewDecoder(listResponse.Body).Decode(&containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(containers).Should(Equal(expectedContainers))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})

			Context("and tags are specified", func() {
				BeforeEach(func() {
					listRequest.URL.RawQuery = url.Values{
						"tag": []string{"a:b", "c:d"},
					}.Encode()
				})

				It("filters by the given tags", func() {
					Ω(depotClient.ListContainersArgsForCall(0)).Should(Equal(executor.Tags{
						"a": "b",
						"c": "d",
					}))
				})
			})
		})

		Context("when we cannot get containers", func() {
			var listResponse *http.Response

			BeforeEach(func() {
				depotClient.ListContainersReturns([]executor.Container{}, errors.New("ahh!"))

				listResponse = DoRequest(generator.CreateRequest(
					ehttp.ListContainers,
					nil,
					nil,
				))
			})

			It("returns 500", func() {
				Ω(listResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.list-handler.failed-to-list-container",
					"test.request.done",
				}))
			})
		})
	})

	Describe("DELETE /containers/:guid", func() {
		var deleteResponse *http.Response

		JustBeforeEach(func() {
			deleteResponse = DoRequest(generator.CreateRequest(
				ehttp.DeleteContainer,
				rata.Params{"guid": containerGuid},
				nil,
			))
		})

		Context("when the container exists", func() {
			It("returns 200 OK", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusOK))
			})

			It("deletes the container", func() {
				Ω(depotClient.DeleteContainerCallCount()).Should(Equal(1))
				Ω(depotClient.DeleteContainerArgsForCall(0)).Should(Equal(containerGuid))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
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
				depotClient.DeleteContainerReturns(executor.ErrContainerNotFound)
			})

			It("returns a 404", func() {
				Ω(deleteResponse.StatusCode).Should(Equal(http.StatusNotFound))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.delete-handler.failed-to-delete-container",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /containers/:guid/files", func() {
		var request *http.Request
		var response *http.Response

		BeforeEach(func() {
			var err error

			request, err = generator.CreateRequest(
				ehttp.GetFiles,
				rata.Params{"guid": containerGuid},
				nil,
			)
			Ω(err).ShouldNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			response = DoRequest(request, nil)
		})

		Context("when the container exists", func() {
			Context("when streaming out of the container succeeds", func() {
				var responseStream *gbytes.Buffer

				BeforeEach(func() {
					responseStream = gbytes.BufferWithBytes([]byte("some-stream"))
					depotClient.GetFilesReturns(responseStream, nil)
				})

				It("returns 200 OK", func() {
					Ω(response.StatusCode).Should(Equal(http.StatusOK))
				})

				It("streams the files to the request", func() {
					streamedOut, err := ioutil.ReadAll(response.Body)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(string(streamedOut)).Should(Equal("some-stream"))
				})

				It("gets the files out of the container's working directory", func() {
					Ω(depotClient.GetFilesCallCount()).Should(Equal(1))

					guid, source := depotClient.GetFilesArgsForCall(0)
					Ω(guid).Should(Equal(containerGuid))
					Ω(source).Should(Equal(""))
				})

				It("returns application/tar content-type", func() {
					Ω(response.Header.Get("content-type")).Should(Equal("application/x-tar"))
				})

				It("closes the stream after the request completes", func() {
					Ω(responseStream.Closed()).Should(BeTrue())
				})

				It("logs the request", func() {
					Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
						"test.request.serving",
						"test.request.done",
					}))
				})

				Context("when a source query param is specified", func() {
					BeforeEach(func() {
						request.URL.RawQuery = url.Values{
							"source": []string{"path/to/file"},
						}.Encode()
					})

					It("gets the files out of the given path in the container", func() {
						Ω(depotClient.GetFilesCallCount()).Should(Equal(1))

						guid, source := depotClient.GetFilesArgsForCall(0)
						Ω(guid).Should(Equal(containerGuid))
						Ω(source).Should(Equal("path/to/file"))
					})
				})
			})

			Context("when streaming out of the container fails", func() {
				BeforeEach(func() {
					depotClient.GetFilesReturns(nil, errors.New("oh no!"))
				})

				It("returns a 500", func() {
					Ω(response.StatusCode).Should(Equal(http.StatusInternalServerError))
				})

				It("logs the request", func() {
					Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
						"test.request.serving",
						"test.request.get-files-handler.failed-to-get-container",
						"test.request.done",
					}))
				})
			})
		})

		Context("when the container doesn't exist", func() {
			BeforeEach(func() {
				depotClient.GetFilesReturns(nil, executor.ErrContainerNotFound)
			})

			It("returns a 404", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusNotFound))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-files-handler.failed-to-get-container",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /containers/:guid/metrics", func() {
		var request *http.Request
		var response *http.Response

		BeforeEach(func() {
			var err error

			request, err = generator.CreateRequest(
				ehttp.GetMetrics,
				rata.Params{"guid": containerGuid},
				nil,
			)
			Ω(err).ShouldNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			response = DoRequest(request, nil)
		})

		It("sets the content type to application/json", func() {
			Ω(response.Header.Get("Content-Type")).Should(Equal("application/json"))
		})

		Context("when the container exists", func() {
			var metrics executor.ContainerMetrics

			BeforeEach(func() {
				metrics = executor.ContainerMetrics{
					MemoryUsageInBytes: 1234,
					DiskUsageInBytes:   5678,
					TimeSpentInCPU:     112358,
				}
				depotClient.GetMetricsReturns(metrics, nil)
			})

			It("returns a 200", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the expected metrics", func() {
				result := executor.ContainerMetrics{}

				err := json.NewDecoder(response.Body).Decode(&result)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(result).Should(Equal(metrics))
			})
		})

		Context("when the container does not exist", func() {
			BeforeEach(func() {
				depotClient.GetMetricsReturns(executor.ContainerMetrics{}, executor.ErrContainerNotFound)
			})

			It("returns a 404", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusNotFound))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-metrics-handler.container-not-found",
					"test.request.done",
				}))
			})
		})

		Context("when an unexpected error occurs", func() {
			BeforeEach(func() {
				depotClient.GetMetricsReturns(executor.ContainerMetrics{}, errors.New("woops"))
			})

			It("returns 500", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-metrics-handler.failed-to-get-metrics",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /metrics", func() {
		var request *http.Request
		var response *http.Response

		BeforeEach(func() {
			var err error

			request, err = generator.CreateRequest(
				ehttp.GetAllMetrics,
				nil,
				nil,
			)
			Ω(err).ShouldNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			response = DoRequest(request, nil)
		})

		It("sets the content type to application/json", func() {
			Ω(response.Header.Get("Content-Type")).Should(Equal("application/json"))
		})

		Context("when no tags", func() {
			var metrics map[string]executor.Metrics

			BeforeEach(func() {
				metrics = map[string]executor.Metrics{
					"a-guid": executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: 123,
							DiskUsageInBytes:   456,
							TimeSpentInCPU:     100 * time.Second,
						},
					},
				}
				depotClient.GetAllMetricsReturns(metrics, nil)
			})

			It("calls GetAllMetrics with no tags", func() {
				Ω(depotClient.GetAllMetricsCallCount()).Should(Equal(1))
				Ω(depotClient.GetAllMetricsArgsForCall(0)).Should(Equal(executor.Tags{}))
			})

			It("returns a 200", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the expected metrics", func() {
				result := map[string]executor.Metrics{}

				err := json.NewDecoder(response.Body).Decode(&result)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(result).Should(Equal(metrics))
			})
		})

		Context("when tags", func() {
			var metrics map[string]executor.Metrics

			BeforeEach(func() {
				request.URL.RawQuery = url.Values{"tag": []string{"a:b"}}.Encode()
				metrics = map[string]executor.Metrics{
					"a-guid": executor.Metrics{
						MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"},
						ContainerMetrics: executor.ContainerMetrics{
							MemoryUsageInBytes: 123,
							DiskUsageInBytes:   456,
							TimeSpentInCPU:     100 * time.Second,
						},
					},
				}
				depotClient.GetAllMetricsReturns(metrics, nil)
			})

			It("calls GetAllMetrics with tags", func() {
				Ω(depotClient.GetAllMetricsCallCount()).Should(Equal(1))
				Ω(depotClient.GetAllMetricsArgsForCall(0)).Should(Equal(executor.Tags{"a": "b"}))
			})

			It("returns a 200", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})

			It("returns the expected metrics", func() {
				result := map[string]executor.Metrics{}

				err := json.NewDecoder(response.Body).Decode(&result)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(result).Should(Equal(metrics))
			})
		})

		Context("when an unexpected error occurs", func() {
			BeforeEach(func() {
				depotClient.GetAllMetricsReturns(nil, errors.New("woops"))
			})

			It("returns 500", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.get-all-metrics-handler.failed-to-get-metrics",
					"test.request.done",
				}))
			})
		})
	})
	Describe("GET /resources/remaining", func() {
		var resourcesResponse *http.Response

		JustBeforeEach(func() {
			resourcesResponse = DoRequest(generator.CreateRequest(
				ehttp.GetRemainingResources,
				nil,
				nil,
			))
		})

		Context("when we can determine remaining resources", func() {
			var expectedExecutorResources executor.ExecutorResources

			BeforeEach(func() {
				expectedExecutorResources = executor.ExecutorResources{
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
				var resources executor.ExecutorResources
				err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusOK))
				Ω(resources).Should(Equal(expectedExecutorResources))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})
		})

		Context("when we cannot determine remaining resources", func() {
			BeforeEach(func() {
				depotClient.RemainingResourcesReturns(executor.ExecutorResources{}, errors.New("BOOM"))
			})

			It("returns 500", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.remaining-resources-handler.failed-to-get-remaining-resources",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /resources/total", func() {
		var resourcesResponse *http.Response
		var expectedTotalResources executor.ExecutorResources

		JustBeforeEach(func() {
			resourcesResponse = DoRequest(generator.CreateRequest(
				ehttp.GetTotalResources,
				nil,
				nil,
			))
		})

		Context("when total resources are available", func() {
			BeforeEach(func() {
				expectedTotalResources = executor.ExecutorResources{
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
				var resources executor.ExecutorResources
				err := json.NewDecoder(resourcesResponse.Body).Decode(&resources)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(resources).Should(Equal(expectedTotalResources))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})
		})

		Context("when we cannot determine the total resources", func() {
			BeforeEach(func() {
				depotClient.TotalResourcesReturns(executor.ExecutorResources{}, errors.New("BOOM"))
			})

			It("returns 500", func() {
				Ω(resourcesResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
			})

			It("logs the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.total-resources-handler.failed-to-get-total-resources",
					"test.request.done",
				}))
			})
		})
	})

	Describe("GET /ping", func() {
		Context("when Garden responds to ping", func() {
			BeforeEach(func() {
				depotClient.PingReturns(nil)
			})

			It("should 200", func() {
				response := DoRequest(generator.CreateRequest(ehttp.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})

			It("does not log the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(BeEmpty())
			})
		})

		Context("when Garden returns an error", func() {
			BeforeEach(func() {
				depotClient.PingReturns(errors.New("oh no!"))
			})

			It("should 502", func() {
				response := DoRequest(generator.CreateRequest(ehttp.Ping, nil, nil))
				Ω(response.StatusCode).Should(Equal(http.StatusBadGateway))
			})

			It("does not log the request", func() {
				Ω(logger.TestSink.LogMessages()).Should(BeEmpty())
			})
		})
	})

	Describe("GET /events", func() {
		var response *http.Response

		Context("when the depot emits events", func() {
			var fakeEventSource *fakes.FakeEventSource

			event1 := executor.ContainerCompleteEvent{
				executor.Container{
					Guid:   "the-guid",
					Action: action,

					RunResult: executor.ContainerRunResult{
						Failed:        true,
						FailureReason: "i hit my head",
					},
				},
			}

			event2 := executor.ContainerCompleteEvent{
				executor.Container{
					Guid:   "a-guid",
					Action: action,

					RunResult: executor.ContainerRunResult{
						Failed: false,
					},
				},
			}

			BeforeEach(func() {
				events := make(chan executor.Event, 3)
				events <- event1
				events <- event2
				close(events)

				fakeEventSource = new(fakes.FakeEventSource)
				fakeEventSource.NextStub = func() (executor.Event, error) {
					e, ok := <-events
					if ok {
						return e, nil
					}

					return nil, errors.New("nope")
				}

				depotClient.SubscribeToEventsReturns(fakeEventSource, nil)
			})

			JustBeforeEach(func() {
				response = DoRequest(generator.CreateRequest(ehttp.Events, nil, nil))
			})

			It("response with the appropriate SSE headers", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))

				Ω(response.Header.Get("Content-Type")).Should(Equal("text/event-stream; charset=utf-8"))
				Ω(response.Header.Get("Cache-Control")).Should(Equal("no-cache, no-store, must-revalidate"))
				Ω(response.Header.Get("Connection")).Should(Equal("keep-alive"))
			})

			It("emits the events via SSE and ends when the source returns an error", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))

				reader := sse.NewReadCloser(response.Body)

				payload1, err := json.Marshal(event1)
				Ω(err).ShouldNot(HaveOccurred())

				payload2, err := json.Marshal(event2)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(reader.Next()).Should(Equal(sse.Event{
					ID:   "0",
					Name: string(executor.EventTypeContainerComplete),
					Data: payload1,
				}))

				Ω(reader.Next()).Should(Equal(sse.Event{
					ID:   "1",
					Name: string(executor.EventTypeContainerComplete),
					Data: payload2,
				}))

				_, err = reader.Next()
				Ω(err).Should(Equal(io.EOF))
			})

			It("logs the request", func() {
				_, err := ioutil.ReadAll(response.Body)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.request.serving",
					"test.request.done",
				}))
			})

			Context("when the client goes away", func() {
				It("closes the server's source", func() {
					response.Body.Close()

					Eventually(fakeEventSource.CloseCallCount).Should(Equal(1))
				})
			})
		})

		Context("when the depot returns an error", func() {
			BeforeEach(func() {
				depotClient.SubscribeToEventsReturns(nil, errors.New("oh no!"))
				response = DoRequest(generator.CreateRequest(ehttp.Events, nil, nil))
			})

			It("should 502", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusBadGateway))
			})
		})
	})
})
