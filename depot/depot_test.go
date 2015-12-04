package depot_test

import (
	"errors"
	"io"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/depot/containerstore/containerstorefakes"
	efakes "github.com/cloudfoundry-incubator/executor/depot/event/fakes"
	fakes "github.com/cloudfoundry-incubator/executor/depot/fakes"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Depot", func() {
	const (
		defaultMemoryMB = 256
		defaultDiskMB   = 256
	)

	var (
		depotClient    executor.Client
		logger         lager.Logger
		fakeClock      *fakeclock.FakeClock
		eventHub       *efakes.FakeHub
		gardenStore    *fakes.FakeGardenStore
		containerStore *containerstorefakes.FakeContainerStore
		resources      executor.ExecutorResources
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")
		fakeClock = fakeclock.NewFakeClock(time.Now())
		eventHub = new(efakes.FakeHub)
		gardenStore = new(fakes.FakeGardenStore)
		containerStore = new(containerstorefakes.FakeContainerStore)

		resources = executor.ExecutorResources{
			MemoryMB:   1024,
			DiskMB:     1024,
			Containers: 3,
		}

		workPoolSettings := executor.WorkPoolSettings{
			CreateWorkPoolSize:  5,
			DeleteWorkPoolSize:  5,
			ReadWorkPoolSize:    5,
			MetricsWorkPoolSize: 5,
		}

		d, err := depot.NewClientProvider(resources, containerStore, gardenStore, eventHub, workPoolSettings)
		Expect(err).NotTo(HaveOccurred())

		depotClient = d.WithLogger(logger)
	})

	Describe("AllocateContainers", func() {
		Context("when allocating a single valid container within executor resource limits", func() {
			var requests []executor.AllocationRequest
			BeforeEach(func() {
				requests = []executor.AllocationRequest{
					newAllocationRequest("guid-1", 512, 512),
				}
			})

			It("should allocate the container", func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				Expect(containerStore.ReserveCallCount()).To(Equal(1))
				_, request := containerStore.ReserveArgsForCall(0)
				Expect(*request).To(Equal(requests[0]))
			})
		})

		Context("when allocating multiple valid containers", func() {
			var requests []executor.AllocationRequest

			BeforeEach(func() {
				requests = []executor.AllocationRequest{
					newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
					newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
					newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
				}
			})

			It("should allocate all the containers", func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				Expect(containerStore.ReserveCallCount()).To(Equal(3))
				_, request := containerStore.ReserveArgsForCall(0)
				Expect(*request).To(Equal(requests[0]))

				_, request = containerStore.ReserveArgsForCall(1)
				Expect(*request).To(Equal(requests[1]))

				_, request = containerStore.ReserveArgsForCall(2)
				Expect(*request).To(Equal(requests[2]))
			})
		})

		Context("when the container store returns an error while allocating", func() {
			var requests []executor.AllocationRequest

			BeforeEach(func() {
				requests = []executor.AllocationRequest{
					newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
					newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
				}

				containerStore.ReserveStub = func(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error) {
					switch req.Guid {
					case "guid-1":
						return executor.Container{}, executor.ErrContainerGuidNotAvailable
					case "guid-2":
						return executor.Container{}, nil
					default:
						return executor.Container{}, errors.New("unexpected input")
					}
				}
			})

			It("should not allocate container with duplicate guid", func() {
				failures, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())

				Expect(failures).To(HaveLen(1))
				expectedFailure := executor.NewAllocationFailure(&requests[0], executor.ErrContainerGuidNotAvailable.Error())
				Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

				Expect(containerStore.ReserveCallCount()).To(Equal(2))

				_, request := containerStore.ReserveArgsForCall(0)
				Expect(*request).To(Equal(requests[0]))
				_, request = containerStore.ReserveArgsForCall(1)
				Expect(*request).To(Equal(requests[1]))
			})
		})

		Context("when one of the containers has empty guid", func() {
			var requests []executor.AllocationRequest

			BeforeEach(func() {
				requests = []executor.AllocationRequest{
					newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
					newAllocationRequest("", defaultMemoryMB, defaultDiskMB),
				}
			})

			It("should not allocate container with empty guid", func() {
				failures, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(failures).To(HaveLen(1))
				expectedFailure := executor.NewAllocationFailure(&requests[1], executor.ErrGuidNotSpecified.Error())
				Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

				Expect(containerStore.ReserveCallCount()).To(Equal(1))

				_, request := containerStore.ReserveArgsForCall(0)
				Expect(*request).To(Equal(requests[0]))
			})
		})
	})

	Describe("RunContainer", func() {
		var (
			gardenStoreGuid string
			allocRequests   []executor.AllocationRequest
			runRequest      *executor.RunRequest
		)

		BeforeEach(func() {
			gardenStoreGuid = "garden-store-guid"
			allocRequests = []executor.AllocationRequest{
				newAllocationRequest(gardenStoreGuid, 512, 512),
			}
			runRequest = newRunRequest(gardenStoreGuid)
		})

		Context("when the container is valid", func() {
			BeforeEach(func() {
				containerStore.InitializeReturns(nil)
			})

			It("should create garden container, run it, and remove from allocation store", func() {
				err := depotClient.RunContainer(runRequest)
				Expect(err).NotTo(HaveOccurred())

				Expect(containerStore.InitializeCallCount()).To(Equal(1))
				_, req := containerStore.InitializeArgsForCall(0)
				Expect(req).To(Equal(runRequest))

				Eventually(containerStore.CreateCallCount).Should(Equal(1))
				Eventually(containerStore.RunCallCount).Should(Equal(1))
				_, guid := containerStore.CreateArgsForCall(0)
				Expect(guid).To(Equal(gardenStoreGuid))

				_, guid = containerStore.RunArgsForCall(0)
				Expect(guid).To(Equal(gardenStoreGuid))
			})
		})

		Context("when the container fails to initialize", func() {
			BeforeEach(func() {
				containerStore.InitializeReturns(executor.ErrContainerNotFound)
			})

			It("should return error", func() {
				err := depotClient.RunContainer(newRunRequest("missing-guid"))
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})

		Context("when creating the container fails", func() {
			BeforeEach(func() {
				containerStore.InitializeReturns(nil)
				containerStore.CreateReturns(executor.Container{}, errors.New("some-error"))
			})

			It("returns an error", func() {
				err := depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when running the container fails", func() {
			BeforeEach(func() {
				containerStore.RunReturns(errors.New("some-error"))
			})

			It("should log the error", func() {
				err := depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				Expect(err).NotTo(HaveOccurred())

				Eventually(containerStore.RunCallCount).Should(Equal(1))
				Expect(logger).To(gbytes.Say("test.depot-client.run-container.failed-running-container-in-garden"))
			})
		})
	})

	Describe("Throttling", func() {
		var (
			numRequests      int
			gardenStoreGuid  = "garden-store-guid"
			workPoolSettings executor.WorkPoolSettings
		)

		BeforeEach(func() {
			numRequests = 10
			resources = executor.ExecutorResources{
				MemoryMB:   1024,
				DiskMB:     1024,
				Containers: 10,
			}

			workPoolSettings = executor.WorkPoolSettings{
				CreateWorkPoolSize:  2,
				DeleteWorkPoolSize:  6,
				ReadWorkPoolSize:    4,
				MetricsWorkPoolSize: 5,
			}

			d, err := depot.NewClientProvider(resources, containerStore, gardenStore, eventHub, workPoolSettings)
			Expect(err).NotTo(HaveOccurred())

			depotClient = d.WithLogger(logger)
		})

		Context("Container creation", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, numRequests)
				doneChan = make(chan struct{})

				containerStore.CreateStub = func(logger lager.Logger, guid string) (executor.Container, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return executor.Container{}, nil
				}
			})

			It("throttles the requests to Garden", func() {
				for i := 0; i < numRequests; i++ {
					go depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				}

				Eventually(containerStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize))
				Consistently(containerStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize))

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.CreateWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.CreateWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(containerStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize + 1))
				Consistently(containerStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize + 1))

				close(doneChan)
				Eventually(containerStore.CreateCallCount).Should(Equal(numRequests))
			})
		})

		Context("Container Deletion", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, numRequests)
				doneChan = make(chan struct{})
				containerStore.DestroyStub = func(logger lager.Logger, guid string) error {
					throttleChan <- struct{}{}
					<-doneChan
					return nil
				}
				containerStore.StopStub = func(logger lager.Logger, guid string) error {
					throttleChan <- struct{}{}
					<-doneChan
					return nil
				}
			})

			It("throttles the requests to Garden", func() {
				deleteContainerCount := 0
				for i := 0; i < numRequests; i++ {
					deleteContainerCount++
					go depotClient.DeleteContainer(gardenStoreGuid)
				}

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize))

				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(func() int {
					return containerStore.StopCallCount() + containerStore.DestroyCallCount()
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize + 1))

				close(doneChan)

				Eventually(containerStore.DestroyCallCount).Should(Equal(deleteContainerCount))
			})
		})

		Context("Retrieves containers", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, numRequests)
				doneChan = make(chan struct{})
				containerStore.GetFilesStub = func(logger lager.Logger, guid string, sourcePath string) (io.ReadCloser, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return nil, nil
				}
				containerStore.ListStub = func(logger lager.Logger) []executor.Container {
					throttleChan <- struct{}{}
					<-doneChan
					return []executor.Container{executor.Container{}}
				}
			})

			It("throttles the requests to Garden", func() {
				getFilesCount := 0
				for i := 0; i < numRequests; i++ {
					getFilesCount++
					go depotClient.GetFiles(gardenStoreGuid, "/some/path")
				}

				Eventually(throttleChan).Should(HaveLen(workPoolSettings.ReadWorkPoolSize))
				Consistently(throttleChan).Should(HaveLen(workPoolSettings.ReadWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(func() int {
					return containerStore.ListCallCount() + containerStore.GetFilesCallCount()
				}).Should(Equal(workPoolSettings.ReadWorkPoolSize + 1))

				close(doneChan)

				Eventually(containerStore.GetFilesCallCount).Should(Equal(getFilesCount))
			})
		})

		Context("Metrics", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, numRequests)
				doneChan = make(chan struct{})
				containerStore.MetricsStub = func(logger lager.Logger) (map[string]executor.ContainerMetrics, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return map[string]executor.ContainerMetrics{
						"some-guid": executor.ContainerMetrics{},
					}, nil
				}

				containerStore.ListReturns([]executor.Container{executor.Container{Guid: "some-guid"}})
			})

			It("throttles the requests to Garden", func() {
				for i := 0; i < numRequests; i++ {
					go depotClient.GetBulkMetrics()
				}

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.MetricsWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.MetricsWorkPoolSize))

				doneChan <- struct{}{}
				Eventually(containerStore.MetricsCallCount).Should(Equal(workPoolSettings.MetricsWorkPoolSize + 1))
				close(doneChan)
				Eventually(containerStore.MetricsCallCount).Should(Equal(numRequests))
			})
		})
	})

	Describe("ListContainers", func() {
		var containers []executor.Container
		BeforeEach(func() {
			containers = []executor.Container{
				{Guid: "guid-1"},
				{Guid: "guid-2"},
			}

			containerStore.ListReturns(containers)
		})

		It("lists the containers in the container store", func() {
			returnedContainers, err := depotClient.ListContainers()
			Expect(err).NotTo(HaveOccurred())
			Expect(returnedContainers).To(Equal(containers))
			Expect(containerStore.ListCallCount()).To(Equal(1))
		})
	})

	Describe("GetBulkMetrics", func() {
		var metrics map[string]executor.Metrics
		var metricsErr error

		var expectedMetrics map[string]executor.ContainerMetrics

		BeforeEach(func() {
			expectedMetrics = map[string]executor.ContainerMetrics{
				"a-guid": executor.ContainerMetrics{
					MemoryUsageInBytes: 123,
					DiskUsageInBytes:   456,
					TimeSpentInCPU:     100 * time.Second,
				},
				"b-guid": executor.ContainerMetrics{
					MemoryUsageInBytes: 321,
					DiskUsageInBytes:   654,
					TimeSpentInCPU:     100 * time.Second,
				},
			}

			containerStore.MetricsReturns(expectedMetrics, nil)
		})

		JustBeforeEach(func() {
			metrics, metricsErr = depotClient.GetBulkMetrics()
		})

		Context("with no tags", func() {
			BeforeEach(func() {
				containerStore.ListReturns([]executor.Container{
					executor.Container{Guid: "a-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"}}},
					executor.Container{Guid: "b-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}}},
				})
			})

			It("gets all the containers", func() {
				Expect(containerStore.ListCallCount()).To(Equal(1))
			})

			It("retrieves all the metrics", func() {
				Expect(containerStore.MetricsCallCount()).To(Equal(1))
			})

			It("does not error", func() {
				Expect(metricsErr).NotTo(HaveOccurred())
			})

			It("returns all the metrics", func() {
				Expect(metrics).To(HaveLen(2))
				Expect(metrics["a-guid"]).To(Equal(executor.Metrics{
					MetricsConfig:    executor.MetricsConfig{Guid: "a-metrics"},
					ContainerMetrics: expectedMetrics["a-guid"],
				}))

				Expect(metrics["b-guid"]).To(Equal(executor.Metrics{
					MetricsConfig:    executor.MetricsConfig{Guid: "b-metrics", Index: 1},
					ContainerMetrics: expectedMetrics["b-guid"],
				}))
			})
		})

		Context("containers with missing metric guids", func() {
			BeforeEach(func() {
				containerStore.ListReturns([]executor.Container{
					executor.Container{Guid: "a-guid"},
					executor.Container{Guid: "b-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}}},
				})
			})

			It("does not error", func() {
				Expect(metricsErr).NotTo(HaveOccurred())
			})

			It("returns the metrics", func() {
				Expect(metrics).To(HaveLen(1))
				Expect(metrics["b-guid"]).To(Equal(executor.Metrics{
					MetricsConfig:    executor.MetricsConfig{Guid: "b-metrics", Index: 1},
					ContainerMetrics: expectedMetrics["b-guid"],
				}))
			})
		})

		Context("when garden fails to get the metrics", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				containerStore.MetricsReturns(nil, expectedError)
			})

			It("propagates the error", func() {
				Expect(metricsErr).To(Equal(expectedError))
			})
		})
	})

	Describe("DeleteContainer", func() {
		var requests []executor.AllocationRequest

		BeforeEach(func() {
			requests = []executor.AllocationRequest{
				newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
				newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
				newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
			}
		})

		It("removes the container from the container store", func() {
			err := depotClient.DeleteContainer("guid-1")
			Expect(err).NotTo(HaveOccurred())

			Expect(containerStore.DestroyCallCount()).To(Equal(1))
			_, guid := containerStore.DestroyArgsForCall(0)
			Expect(guid).To(Equal("guid-1"))
		})

		Context("when garden store returns an error", func() {
			BeforeEach(func() {
				containerStore.DestroyReturns(errors.New("some-error"))
			})

			It("should return an error", func() {
				Expect(containerStore.DestroyCallCount()).To(Equal(0))
				err := depotClient.DeleteContainer("guid-1")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("StopContainer", func() {
		var stopError error
		var stopGuid string

		BeforeEach(func() {
			stopGuid = "some-guid"
		})

		JustBeforeEach(func() {
			stopError = depotClient.StopContainer(stopGuid)
		})

		It("stops the container in the container store", func() {
			Expect(stopError).NotTo(HaveOccurred())
			Expect(containerStore.StopCallCount()).To(Equal(1))
			_, guid := containerStore.StopArgsForCall(0)
			Expect(guid).To(Equal(stopGuid))
		})

		Context("when the container store fails to stop the container", func() {
			BeforeEach(func() {
				containerStore.StopReturns(errors.New("boom!"))
			})

			It("returns the error", func() {
				Expect(stopError).To(Equal(errors.New("boom!")))
			})
		})
	})

	Describe("GetContainer", func() {
		var container executor.Container

		BeforeEach(func() {
			container = executor.Container{Guid: "the-container-guid"}
			containerStore.GetReturns(container, nil)
		})

		It("retrieves the container from the container store", func() {
			fetchedContainer, err := depotClient.GetContainer("the-container-guid")
			Expect(err).NotTo(HaveOccurred())
			Expect(fetchedContainer).To(Equal(container))

			Expect(containerStore.GetCallCount()).To(Equal(1))
			_, guid := containerStore.GetArgsForCall(0)
			Expect(guid).To(Equal("the-container-guid"))
		})

		Context("when fetching the container from the container store fails", func() {
			BeforeEach(func() {
				containerStore.GetReturns(executor.Container{}, errors.New("failed-to-get-container"))
			})

			It("returns the error", func() {
				_, err := depotClient.GetContainer("any-guid")
				Expect(err).To(Equal(errors.New("failed-to-get-container")))
			})
		})
	})

	Describe("RemainingResources", func() {
		var resources executor.ExecutorResources

		BeforeEach(func() {
			resources = executor.NewExecutorResources(1024, 1024, 3)
			containerStore.RemainingResourcesReturns(resources)
		})

		It("should reduce resources used by allocated and running containers", func() {
			actualResources, err := depotClient.RemainingResources()
			Expect(err).NotTo(HaveOccurred())
			Expect(actualResources).To(Equal(resources))
		})
	})

	Describe("TotalResources", func() {
		Context("when asked for total resources", func() {
			It("should return the resources it was configured with", func() {
				Expect(depotClient.TotalResources()).To(Equal(resources))
			})
		})
	})
})

func convertSliceToMap(containers []executor.Container) map[string]executor.Container {
	containersMap := map[string]executor.Container{}
	for _, container := range containers {
		containersMap[container.Guid] = container
	}
	return containersMap
}

func newAllocationRequest(guid string, memoryMB, diskMB int, tagses ...executor.Tags) executor.AllocationRequest {
	resource := executor.NewResource(memoryMB, diskMB, "linux")
	var tags executor.Tags
	if len(tagses) > 0 {
		tags = tagses[0]
	}
	return executor.NewAllocationRequest(guid, &resource, tags)
}

func newRunRequest(guid string) *executor.RunRequest {
	runInfo := executor.RunInfo{
	// TODO: Fill in required fields.
	}
	r := executor.NewRunRequest(guid, &runInfo, nil)
	return &r
}

func newRunningContainer(req *executor.RunRequest, res executor.Resource) executor.Container {
	c := executor.NewContainerFromResource(req.Guid, &res, req.Tags)
	c.State = executor.StateRunning
	c.RunInfo = req.RunInfo
	return c
}
