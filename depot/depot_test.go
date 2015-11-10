package depot_test

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore"
	efakes "github.com/cloudfoundry-incubator/executor/depot/event/fakes"
	fakes "github.com/cloudfoundry-incubator/executor/depot/fakes"
	"github.com/cloudfoundry-incubator/executor/depot/keyed_lock/fakelockmanager"
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
		depotClient     executor.Client
		logger          lager.Logger
		fakeClock       *fakeclock.FakeClock
		eventHub        *efakes.FakeHub
		allocationStore depot.AllocationStore
		gardenStore     *fakes.FakeGardenStore
		resources       executor.ExecutorResources
		lockManager     *fakelockmanager.FakeLockManager
	)

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		fakeClock = fakeclock.NewFakeClock(time.Now())

		eventHub = new(efakes.FakeHub)

		allocationStore = allocationstore.NewAllocationStore(fakeClock, eventHub)

		gardenStore = new(fakes.FakeGardenStore)

		resources = executor.ExecutorResources{
			MemoryMB:   1024,
			DiskMB:     1024,
			Containers: 3,
		}

		lockManager = &fakelockmanager.FakeLockManager{}
		workPoolSettings := executor.WorkPoolSettings{
			CreateWorkPoolSize:  5,
			DeleteWorkPoolSize:  5,
			ReadWorkPoolSize:    5,
			MetricsWorkPoolSize: 5,
		}

		d, err := depot.NewClientProvider(resources, allocationStore, gardenStore, eventHub, lockManager, workPoolSettings)
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
				allocatedContainers := allocationStore.List()
				Expect(allocatedContainers).To(HaveLen(len(requests)))
				Expect(allocatedContainers[0].Guid).To(Equal("guid-1"))
				Expect(allocatedContainers[0].State).To(Equal(executor.StateReserved))
			})
		})

		Context("when allocating multiple valid containers", func() {
			var requests []executor.AllocationRequest

			Context("when total required resources are within executor resource limits", func() {
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
					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests)))

					allocatedContainersMap := map[string]*executor.Container{}
					for _, container := range allocatedContainers {
						allocatedContainersMap[container.Guid] = &container
					}

					Expect(allocatedContainersMap).To(HaveLen(len(requests)))
					Expect(allocatedContainersMap["guid-1"]).NotTo(BeNil())
					Expect(allocatedContainersMap["guid-1"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-2"]).NotTo(BeNil())
					Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-3"]).NotTo(BeNil())
					Expect(allocatedContainersMap["guid-3"].State).To(Equal(executor.StateReserved))
				})
			})

			Context("when required memory is more than executor resource limits", func() {
				BeforeEach(func() {
					requests = []executor.AllocationRequest{
						newAllocationRequest("guid-1", 512, defaultDiskMB),
						newAllocationRequest("guid-2", 512, defaultDiskMB),
						newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					failures, err := depotClient.AllocateContainers(requests)
					Expect(err).NotTo(HaveOccurred())

					Expect(failures).To(HaveLen(1))
					expectedFailure := executor.NewAllocationFailure(&requests[2], executor.ErrInsufficientResourcesAvailable.Error())
					Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)
					Expect(allocatedContainersMap).To(HaveLen(len(requests) - 1))
					Expect(allocatedContainersMap["guid-1"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap).NotTo(HaveKey("guid-3"))
				})
			})

			Context("when required disk space is more than executor resource limits", func() {
				BeforeEach(func() {
					requests = []executor.AllocationRequest{
						newAllocationRequest("guid-1", defaultMemoryMB, 512),
						newAllocationRequest("guid-2", defaultMemoryMB, 512),
						newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					failures, err := depotClient.AllocateContainers(requests)
					Expect(err).NotTo(HaveOccurred())

					Expect(failures).To(HaveLen(1))
					expectedFailure := executor.NewAllocationFailure(&requests[2], executor.ErrInsufficientResourcesAvailable.Error())
					Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)

					Expect(allocatedContainersMap).To(HaveLen(len(requests) - 1))
					Expect(allocatedContainersMap["guid-1"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap).NotTo(HaveKey("guid-3"))
				})
			})

			Context("when required number of containers is more than what executor can allocate", func() {
				BeforeEach(func() {
					requests = []executor.AllocationRequest{
						newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
						newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
						newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
						newAllocationRequest("guid-4", defaultMemoryMB, defaultDiskMB),
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					failures, err := depotClient.AllocateContainers(requests)
					Expect(err).NotTo(HaveOccurred())

					Expect(failures).To(HaveLen(1))
					expectedFailure := executor.NewAllocationFailure(&requests[3], executor.ErrInsufficientResourcesAvailable.Error())
					Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)
					Expect(allocatedContainersMap).To(HaveLen(len(requests) - 1))
					Expect(allocatedContainersMap["guid-1"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap["guid-3"].State).To(Equal(executor.StateReserved))
					Expect(allocatedContainersMap).NotTo(HaveKey("guid-4"))
				})
			})
		})

		Context("when allocating invalid containers list", func() {
			var requests []executor.AllocationRequest

			Context("when two containers have the same guid", func() {
				BeforeEach(func() {
					requests = []executor.AllocationRequest{
						newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
						newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
					}
				})

				It("should not allocate container with duplicate guid", func() {
					failures, err := depotClient.AllocateContainers(requests)
					Expect(err).NotTo(HaveOccurred())

					Expect(failures).To(HaveLen(1))
					expectedFailure := executor.NewAllocationFailure(&requests[1], executor.ErrContainerGuidNotAvailable.Error())
					Expect(failures[0]).To(BeEquivalentTo(expectedFailure))

					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests) - 1))
					Expect(allocatedContainers[0].Guid).To(Equal("guid-1"))
					Expect(allocatedContainers[0].State).To(Equal(executor.StateReserved))
				})
			})

			Context("when one of the containers has empty guid", func() {
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

					allocatedContainers := allocationStore.List()
					Expect(allocatedContainers).To(HaveLen(len(requests) - 1))
					Expect(allocatedContainers[0].Guid).To(Equal("guid-1"))
					Expect(allocatedContainers[0].State).To(Equal(executor.StateReserved))
				})
			})

		})

		Context("when garden store returns an error", func() {
			var requests []executor.AllocationRequest
			BeforeEach(func() {
				gardenStore.ListReturns(nil, errors.New("error"))
				requests = []executor.AllocationRequest{
					newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
					newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
				}
			})

			It("should return error", func() {
				_, err := depotClient.AllocateContainers(requests)
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrFailureToCheckSpace))
			})
		})
	})

	Describe("RunContainer", func() {
		const gardenStoreGuid = "garden-store-guid"
		var (
			allocRequests []executor.AllocationRequest
			runRequest    *executor.RunRequest
		)

		BeforeEach(func() {

			allocRequests = []executor.AllocationRequest{
				newAllocationRequest(gardenStoreGuid, 512, 512),
			}

			runRequest = newRunRequest(gardenStoreGuid)
		})

		JustBeforeEach(func() {
			errMessageMap, err := depotClient.AllocateContainers(allocRequests)
			Expect(err).NotTo(HaveOccurred())
			Expect(errMessageMap).To(BeEmpty())
		})

		Context("when the container is valid", func() {
			It("should create garden container, run it, and remove from allocation store", func() {
				Expect(gardenStore.CreateCallCount()).To(Equal(0))
				Expect(gardenStore.RunCallCount()).To(Equal(0))
				Expect(allocationStore.List()).To(HaveLen(1))
				err := depotClient.RunContainer(runRequest)
				Expect(err).NotTo(HaveOccurred())
				Eventually(gardenStore.CreateCallCount).Should(Equal(1))
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
				Eventually(allocationStore.List).Should(BeEmpty())
			})

			It("allocates and drops the container lock", func() {
				initialLockCount := lockManager.LockCallCount()
				initialUnlockCount := lockManager.UnlockCallCount()

				err := depotClient.RunContainer(runRequest)
				Expect(err).NotTo(HaveOccurred())

				Eventually(gardenStore.RunCallCount).Should(Equal(1))

				Expect(lockManager.LockCallCount()).To(Equal(initialLockCount + 1))
				Expect(lockManager.LockArgsForCall(initialLockCount)).To(Equal(gardenStoreGuid))

				Expect(lockManager.UnlockCallCount()).To(Equal(initialUnlockCount + 1))
				Expect(lockManager.UnlockArgsForCall(initialUnlockCount)).To(Equal(gardenStoreGuid))
			})
		})

		Context("when it tries to run a missing container", func() {
			It("should return error", func() {
				err := depotClient.RunContainer(newRunRequest("missing-guid"))
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})

			It("does not allocate and drop the container lock", func() {
				initialLockCount := lockManager.LockCallCount()
				initialUnlockCount := lockManager.UnlockCallCount()

				err := depotClient.RunContainer(newRunRequest("missing-guid"))
				Expect(err).To(HaveOccurred())

				Expect(lockManager.LockCallCount()).To(Equal(initialLockCount))
				Expect(lockManager.UnlockCallCount()).To(Equal(initialUnlockCount))
			})
		})

		Context("when the allocation store container is not in the initializing state", func() {
			BeforeEach(func() {
				// Fail the container as RunContainer allocates the lock
				lockManager.LockStub = func(key string) {
					allocationStore.Fail(logger, gardenStoreGuid, "failure-reason")
				}
			})

			It("does not create a garden container", func() {
				err := depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				Expect(err).NotTo(HaveOccurred())

				Eventually(logger).Should(gbytes.Say("test.depot-client.run-container.container-state-invalid"))
				Expect(gardenStore.CreateCallCount()).To(Equal(0))
			})
		})

		Context("when garden container creation fails", func() {
			BeforeEach(func() {
				gardenStore.CreateReturns(executor.Container{}, errors.New("some-error"))
			})

			It("should change container's state to failed", func() {
				Expect(gardenStore.CreateCallCount()).To(Equal(0))
				err := depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				Expect(err).NotTo(HaveOccurred())
				Eventually(gardenStore.CreateCallCount).Should(Equal(1))
				Eventually(gardenStore.RunCallCount).Should(Equal(0))
				Eventually(allocationStore.List).Should(HaveLen(1))
				container, err := allocationStore.Lookup(gardenStoreGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.State).To(Equal(executor.StateCompleted))
				Expect(container.RunResult.Failed).To(BeTrue())
			})
		})

		Context("when garden container run fails", func() {
			BeforeEach(func() {
				gardenStore.RunReturns(errors.New("some-error"))
			})
			It("should log the error", func() {
				err := depotClient.RunContainer(newRunRequest(gardenStoreGuid))
				Expect(err).NotTo(HaveOccurred())
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
				Eventually(allocationStore.List).Should(BeEmpty())

				Expect(logger).To(gbytes.Say("test.depot-client.run-container.failed-running-container-in-garden"))
			})
		})
	})

	Describe("Throttling", func() {
		var (
			requests         []executor.AllocationRequest
			gardenStoreGuid  = "garden-store-guid"
			workPoolSettings executor.WorkPoolSettings
		)

		BeforeEach(func() {
			requests = make([]executor.AllocationRequest, 10)
			for i := 0; i < cap(requests); i++ {
				requests[i] = newAllocationRequest(fmt.Sprintf("%s-%d", gardenStoreGuid, i), 5, 5)
			}

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

			d, err := depot.NewClientProvider(resources, allocationStore, gardenStore, eventHub, lockManager, workPoolSettings)
			Expect(err).NotTo(HaveOccurred())

			depotClient = d.WithLogger(logger)
		})

		Context("Container creation", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, len(requests))
				doneChan = make(chan struct{})
				_, err := depotClient.AllocateContainers(requests)
				Expect(err).ShouldNot(HaveOccurred())

				gardenStore.CreateStub = func(logger lager.Logger, container executor.Container) (executor.Container, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return executor.Container{}, nil
				}
			})

			It("throttles the requests to Garden", func() {
				for _, request := range requests {
					go depotClient.RunContainer(newRunRequest(request.Guid))
				}

				Eventually(gardenStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize))
				Consistently(gardenStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize))

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.CreateWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.CreateWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(gardenStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize + 1))
				Consistently(gardenStore.CreateCallCount).Should(Equal(workPoolSettings.CreateWorkPoolSize + 1))

				close(doneChan)
				Eventually(gardenStore.CreateCallCount).Should(Equal(len(requests)))
			})
		})

		Context("Container Deletion", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, len(requests))
				doneChan = make(chan struct{})
				gardenStore.DestroyStub = func(logger lager.Logger, guid string) error {
					throttleChan <- struct{}{}
					<-doneChan
					return nil
				}
				gardenStore.StopStub = func(logger lager.Logger, guid string) error {
					throttleChan <- struct{}{}
					<-doneChan
					return nil
				}
			})

			It("throttles the requests to Garden", func() {
				stopContainerCount := 0
				deleteContainerCount := 0
				for i, request := range requests {
					if i%2 == 0 {
						stopContainerCount++
						go depotClient.StopContainer(request.Guid)
					} else {
						deleteContainerCount++
						go depotClient.DeleteContainer(request.Guid)
					}
				}

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(func() int {
					return gardenStore.StopCallCount() + gardenStore.DestroyCallCount()
				}).Should(Equal(workPoolSettings.DeleteWorkPoolSize + 1))

				close(doneChan)

				Eventually(gardenStore.StopCallCount).Should(Equal(stopContainerCount))
				Eventually(gardenStore.DestroyCallCount).Should(Equal(deleteContainerCount))
			})
		})

		Context("Retrieves containers", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, len(requests))
				doneChan = make(chan struct{})
				gardenStore.GetFilesStub = func(logger lager.Logger, guid string, sourcePath string) (io.ReadCloser, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return nil, nil
				}
				gardenStore.ListStub = func(logger lager.Logger, tags executor.Tags) ([]executor.Container, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return []executor.Container{executor.Container{}}, nil
				}
				gardenStore.LookupStub = func(logger lager.Logger, guid string) (executor.Container, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return executor.Container{}, nil
				}
			})

			It("throttles the requests to Garden", func() {
				getContainerCount := 0
				listContainerCount := 0
				getFilesCount := 0
				for i, request := range requests {
					switch i % 3 {
					case 0:
						getContainerCount++
						go depotClient.GetContainer(request.Guid)
					case 1:
						listContainerCount++
						go depotClient.ListContainers(executor.Tags{})
					case 2:
						getFilesCount++
						go depotClient.GetFiles(request.Guid, "/some/path")
					}
				}

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.ReadWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.ReadWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(func() int {
					return gardenStore.LookupCallCount() + gardenStore.ListCallCount() + gardenStore.GetFilesCallCount()
				}).Should(Equal(workPoolSettings.ReadWorkPoolSize + 1))

				close(doneChan)

				Eventually(gardenStore.LookupCallCount).Should(Equal(getContainerCount))
				Eventually(gardenStore.ListCallCount).Should(Equal(listContainerCount))
				Eventually(gardenStore.GetFilesCallCount).Should(Equal(getFilesCount))
			})
		})

		Context("Metrics", func() {
			var (
				throttleChan chan struct{}
				doneChan     chan struct{}
			)

			BeforeEach(func() {
				throttleChan = make(chan struct{}, len(requests))
				doneChan = make(chan struct{})
				gardenStore.MetricsStub = func(logger lager.Logger, guids []string) (map[string]executor.ContainerMetrics, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return map[string]executor.ContainerMetrics{
						"some-guid": executor.ContainerMetrics{},
					}, nil
				}
				gardenStore.ListReturns([]executor.Container{executor.Container{Guid: "some-guid"}}, nil)

				gardenStore.LookupStub = func(logger lager.Logger, guid string) (executor.Container, error) {
					throttleChan <- struct{}{}
					<-doneChan
					return executor.Container{}, nil
				}
			})

			It("throttles the requests to Garden", func() {
				for i, request := range requests {
					switch i % 2 {
					case 0:
						go depotClient.GetMetrics(request.Guid)
					case 1:
						go depotClient.GetAllMetrics(executor.Tags{})
					}
				}

				Eventually(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.MetricsWorkPoolSize))
				Consistently(func() int {
					return len(throttleChan)
				}).Should(Equal(workPoolSettings.MetricsWorkPoolSize))

				doneChan <- struct{}{}

				Eventually(gardenStore.MetricsCallCount).Should(Equal(workPoolSettings.MetricsWorkPoolSize + 1))

				close(doneChan)

				Eventually(gardenStore.MetricsCallCount).Should(Equal(len(requests)))
			})
		})
	})

	Describe("ListContainers", func() {
		var requests []executor.AllocationRequest
		BeforeEach(func() {
			requests = []executor.AllocationRequest{
				newAllocationRequest("guid-1", 512, 512, executor.Tags{"a": "a-value", "b": "b-value"}),
				newAllocationRequest("guid-2", 512, 512, executor.Tags{"b": "b-value", "c": "c-value"}),
			}

		})

		Context("when containers exist only allocation store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())
			})

			It("should return the containers from allocation store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(HaveLen(len(requests)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Expect(returnedContainersMap).To(HaveLen(len(requests)))
				Expect(returnedContainersMap["guid-1"].State).To(Equal(executor.StateReserved))
				Expect(returnedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
			})
		})

		Context("when containers exist only garden store", func() {
			var containers []executor.Container

			BeforeEach(func() {
				r := executor.NewResource(512, 512, "")
				containers = []executor.Container{
					newRunningContainer(newRunRequest("guid-1"), r),
					newRunningContainer(newRunRequest("guid-2"), r),
				}
				gardenStore.ListReturns(containers, nil)
			})

			It("should return the containers from garden store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(HaveLen(len(containers)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Expect(returnedContainersMap).To(HaveLen(len(containers)))
				Expect(returnedContainersMap).To(HaveKey("guid-1"))
				Expect(returnedContainersMap).To(HaveKey("guid-2"))
			})
		})

		Context("when containers exist in both allocation store and garden store", func() {
			var containers []executor.Container

			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())
				Expect(allocationStore.List()).Should(HaveLen(2))

				r := executor.NewResource(512, 512, "")
				containers = []executor.Container{
					newRunningContainer(newRunRequest("guid-3"), r),
					newRunningContainer(newRunRequest("guid-4"), r),
				}
				gardenStore.ListReturns(containers, nil)
			})

			It("should return the containers from both the stores", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(HaveLen(4))
				returnedContainersMap := convertSliceToMap(returnedContainers)
				Expect(returnedContainersMap).To(HaveLen(4))
				Expect(returnedContainersMap).To(HaveKey("guid-1"))
				Expect(returnedContainersMap).To(HaveKey("guid-2"))
			})
		})

		Context("when allocation and garden store are empty", func() {
			It("should return empty list", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(BeEmpty())
			})
		})

		Context("when a duplicate container (same guid) exists in both stores", func() {
			var containers []executor.Container

			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				err = depotClient.RunContainer(newRunRequest("guid-1"))
				Expect(err).NotTo(HaveOccurred())

				Eventually(allocationStore.List).Should(HaveLen(1))
				r := executor.NewResource(512, 512, "")
				containers = []executor.Container{
					newRunningContainer(newRunRequest("guid-1"), r),
					newRunningContainer(newRunRequest("guid-1"), r),
				}
				gardenStore.ListReturns(containers, nil)
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
			})

			It("should ignore the duplicate container from garden store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(HaveLen(len(containers)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Expect(returnedContainersMap).To(HaveLen(len(containers)))
				Expect(returnedContainersMap["guid-1"].State).To(Equal(executor.StateRunning))
				Expect(returnedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
			})
		})

		Context("when garden store returns an error", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())
				gardenStore.ListReturns(nil, errors.New("some-error"))
			})

			It("should return an error", func() {
				_, err := depotClient.ListContainers(executor.Tags{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("some-error"))
			})
		})

		Context("when tags are passed", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())
			})

			It("should return the containers matching those tags", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{
					"b": "b-value",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(returnedContainers).To(HaveLen(len(requests)))
				returnedContainersMap := convertSliceToMap(returnedContainers)
				Expect(returnedContainersMap).To(HaveLen(len(requests)))
				Expect(returnedContainersMap).To(HaveKey("guid-1"))
				Expect(returnedContainersMap).To(HaveKey("guid-2"))
			})

			Context("when non-existent tags are passed", func() {
				It("should return empty list", func() {
					returnedContainers, err := depotClient.ListContainers(executor.Tags{
						"non-existent": "any-value",
					})
					Expect(err).NotTo(HaveOccurred())
					Expect(returnedContainers).To(BeEmpty())
				})
			})
		})
	})

	Describe("GetMetrics", func() {
		var expectedMetrics executor.ContainerMetrics

		BeforeEach(func() {
			expectedMetrics = executor.ContainerMetrics{
				MemoryUsageInBytes: 99999,
				DiskUsageInBytes:   88888,
				TimeSpentInCPU:     77777,
			}

			gardenStore.MetricsReturns(map[string]executor.ContainerMetrics{"container-guid": expectedMetrics}, nil)
		})

		It("returns the metrics for the container", func() {
			metrics, err := depotClient.GetMetrics("container-guid")
			Expect(err).NotTo(HaveOccurred())
			Expect(metrics).To(Equal(expectedMetrics))

			Expect(gardenStore.MetricsCallCount()).To(Equal(1))

			actualLogger, guids := gardenStore.MetricsArgsForCall(0)
			Expect(actualLogger).NotTo(BeNil())
			Expect(guids).To(ConsistOf("container-guid"))
		})

		Context("when garden fails to get the metrics", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				gardenStore.MetricsReturns(nil, expectedError)
			})

			It("propagates the error", func() {
				_, err := depotClient.GetMetrics("guid-1")
				Expect(err).To(Equal(expectedError))
			})
		})
	})

	Describe("GetAllMetrics", func() {
		var tags executor.Tags
		var metrics map[string]executor.Metrics
		var metricsErr error

		var expectedMetrics map[string]executor.ContainerMetrics

		BeforeEach(func() {
			tags = nil

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

			gardenStore.MetricsReturns(expectedMetrics, nil)
		})

		JustBeforeEach(func() {
			metrics, metricsErr = depotClient.GetAllMetrics(tags)
		})

		Context("with no tags", func() {
			BeforeEach(func() {
				tags = nil

				gardenStore.ListReturns([]executor.Container{
					executor.Container{Guid: "a-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"}}},
					executor.Container{Guid: "b-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}}},
				}, nil)
			})

			It("gets all the containers", func() {
				Expect(gardenStore.ListCallCount()).To(Equal(1))
				glogger, tags := gardenStore.ListArgsForCall(0)
				Expect(glogger).NotTo(BeNil())
				Expect(tags).To(BeNil())
			})

			It("retrieves all the metrics", func() {
				Expect(gardenStore.MetricsCallCount()).To(Equal(1))

				actualLogger, guids := gardenStore.MetricsArgsForCall(0)
				Expect(actualLogger).NotTo(BeNil())
				Expect(guids).To(ConsistOf("a-guid", "b-guid"))
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

		Context("with tags", func() {
			BeforeEach(func() {
				tags = executor.Tags{"a": "b"}
			})

			It("gets containers with those tags", func() {
				Expect(gardenStore.ListCallCount()).To(Equal(1))
				_, listTags := gardenStore.ListArgsForCall(0)
				Expect(listTags).To(Equal(tags))
			})
		})

		Context("containers with missing metric guids", func() {
			BeforeEach(func() {
				tags = nil

				gardenStore.ListReturns([]executor.Container{
					executor.Container{Guid: "a-guid"},
					executor.Container{Guid: "b-guid", RunInfo: executor.RunInfo{MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}}},
				}, nil)
			})

			It("retrieves metrics by metric config guids", func() {
				Expect(gardenStore.MetricsCallCount()).To(Equal(1))

				actualLogger, guids := gardenStore.MetricsArgsForCall(0)
				Expect(actualLogger).NotTo(BeNil())
				Expect(guids).To(ConsistOf("b-guid"))
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

		Context("when garden fails to list containers", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				gardenStore.ListReturns(nil, expectedError)
			})

			It("propagates the error", func() {
				Expect(metricsErr).To(Equal(expectedError))
			})
		})

		Context("when garden fails to get the metrics", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				gardenStore.MetricsReturns(nil, expectedError)
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

		It("allocates and drops the container lock", func() {
			depotClient.DeleteContainer("guid-1")

			Expect(lockManager.LockCallCount()).To(Equal(1))
			Expect(lockManager.LockArgsForCall(0)).To(Equal("guid-1"))

			Expect(lockManager.UnlockCallCount()).To(Equal(1))
			Expect(lockManager.UnlockArgsForCall(0)).To(Equal("guid-1"))
		})

		Context("when container exists in allocation store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())
				gardenStore.LookupReturns(executor.Container{}, executor.ErrContainerNotFound)
			})

			It("should remove the container from both allocation store, and attempt to remove it from the garden store", func() {
				Expect(gardenStore.DestroyCallCount()).To(Equal(0))

				err := depotClient.DeleteContainer("guid-1")
				Expect(err).NotTo(HaveOccurred())

				allocatedContainers := allocationStore.List()
				Expect(allocatedContainers).To(HaveLen(2))
				allocatedContainersMap := convertSliceToMap(allocatedContainers)
				Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
				Expect(allocatedContainersMap["guid-3"].State).To(Equal(executor.StateReserved))
				Expect(allocatedContainersMap).NotTo(HaveKey("guid-1"))
				Expect(gardenStore.DestroyCallCount()).To(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Expect(guid).To(Equal("guid-1"))
			})
		})

		Context("when container exists in garden store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				err = depotClient.RunContainer(newRunRequest("guid-1"))
				Expect(err).NotTo(HaveOccurred())
				Eventually(allocationStore.List).Should(HaveLen(2))
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
			})

			It("should remove the container from both allocation store and the garden store", func() {
				Expect(gardenStore.DestroyCallCount()).To(Equal(0))

				err := depotClient.DeleteContainer("guid-1")
				Expect(err).NotTo(HaveOccurred())

				allocatedContainers := allocationStore.List()
				Expect(allocatedContainers).To(HaveLen(2))
				allocatedContainersMap := convertSliceToMap(allocatedContainers)
				Expect(allocatedContainersMap["guid-2"].State).To(Equal(executor.StateReserved))
				Expect(allocatedContainersMap["guid-3"].State).To(Equal(executor.StateReserved))
				Expect(allocatedContainersMap).NotTo(HaveKey("guid-1"))
				Expect(gardenStore.DestroyCallCount()).To(Equal(1))
				Expect(gardenStore.DestroyCallCount()).To(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Expect(guid).To(Equal("guid-1"))
			})
		})

		Context("when garden store returns an error", func() {
			BeforeEach(func() {
				gardenStore.DestroyReturns(errors.New("some-error"))
			})

			It("should return an error", func() {
				Expect(gardenStore.DestroyCallCount()).To(Equal(0))
				err := depotClient.DeleteContainer("guid-1")
				Expect(err).To(HaveOccurred())
				Expect(gardenStore.DestroyCallCount()).To(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Expect(guid).To(Equal("guid-1"))
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

		It("allocates and drops the container lock", func() {
			Expect(lockManager.LockCallCount()).To(Equal(1))
			Expect(lockManager.LockArgsForCall(0)).To(Equal(stopGuid))

			Expect(lockManager.UnlockCallCount()).To(Equal(1))
			Expect(lockManager.UnlockArgsForCall(0)).To(Equal(stopGuid))
		})

		Context("when the container doesn't exist in the allocation store", func() {
			It("should stop the garden container", func() {
				Expect(stopError).NotTo(HaveOccurred())
				Expect(gardenStore.StopCallCount()).To(Equal(1))
			})
		})

		Context("when the container is in the allocation store", func() {
			BeforeEach(func() {
				req := newAllocationRequest(stopGuid, 512, 512)
				_, err := allocationStore.Allocate(logger, &req)
				Expect(err).NotTo(HaveOccurred())
			})

			Context("when the container is not completed", func() {
				It("should fail the container", func() {
					result, err := allocationStore.Lookup(stopGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.State).To(Equal(executor.StateCompleted))
					Expect(result.RunResult.Failed).To(BeTrue())
					Expect(result.RunResult.FailureReason).To(Equal(depot.ContainerStoppedBeforeRunMessage))
				})
			})

			Context("when the container is completed", func() {
				BeforeEach(func() {
					_, err := allocationStore.Fail(logger, stopGuid, "go away")
					Expect(err).NotTo(HaveOccurred())
				})

				It("the run result should remain unchanged", func() {
					result, err := allocationStore.Lookup(stopGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.State).To(Equal(executor.StateCompleted))
					Expect(result.RunResult.Failed).To(BeTrue())
					Expect(result.RunResult.FailureReason).To(Equal("go away"))
				})
			})
		})

		Context("when the container is present in gardenStore", func() {
			It("should successfully stop the container", func() {
				Expect(stopError).NotTo(HaveOccurred())
				Expect(gardenStore.StopCallCount()).To(Equal(1))
			})
		})

		Context("when the container is absent in gardenStore", func() {
			BeforeEach(func() {
				gardenStore.StopReturns(errors.New("some-error"))
			})

			It("should fail and return an error", func() {
				Expect(stopError).To(HaveOccurred())
				Expect(stopError.Error()).To(Equal("some-error"))
				Expect(gardenStore.StopCallCount()).To(Equal(1))
			})
		})
	})

	Describe("GetContainer", func() {
		var (
			requests []executor.AllocationRequest

			gardenStoreGuid     string
			allocationStoreGuid string
		)

		BeforeEach(func() {
			gardenStoreGuid = "garden-store-guid"
			allocationStoreGuid = "allocation-store-guid"
			requests = []executor.AllocationRequest{
				newAllocationRequest(gardenStoreGuid, 512, 512),
				newAllocationRequest(allocationStoreGuid, 512, 512),
			}

			errMessageMap, err := depotClient.AllocateContainers(requests)
			Expect(err).NotTo(HaveOccurred())
			Expect(errMessageMap).To(BeEmpty())
			Expect(gardenStore.CreateCallCount()).To(Equal(0))
			Expect(gardenStore.RunCallCount()).To(Equal(0))
			Expect(allocationStore.List()).To(HaveLen(2))
			err = depotClient.RunContainer(newRunRequest(gardenStoreGuid))
			Expect(err).NotTo(HaveOccurred())
			Eventually(gardenStore.CreateCallCount).Should(Equal(1))
			Eventually(gardenStore.RunCallCount).Should(Equal(1))
			Eventually(allocationStore.List).Should(HaveLen(1))

			gardenStore.LookupReturns(executor.Container{
				Guid: gardenStoreGuid,
				Resource: executor.Resource{
					MemoryMB: 512,
					DiskMB:   512,
				},
			},
				nil,
			)
		})

		Context("when container exists in allocation store", func() {
			It("should return the container", func() {
				container, err := depotClient.GetContainer(allocationStoreGuid)
				Expect(err).NotTo(HaveOccurred())
				Expect(container.Guid).To(Equal(allocationStoreGuid))
				Expect(container.State).To(Equal(executor.StateReserved))
			})
		})

		Context("when the container does not exist in the allocation store", func() {
			It("allocates and drops the container lock", func() {
				initialLockCount := lockManager.LockCallCount()
				initialUnlockCount := lockManager.UnlockCallCount()

				_, err := depotClient.GetContainer(gardenStoreGuid)
				Expect(err).NotTo(HaveOccurred())

				Expect(lockManager.LockCallCount()).To(Equal(initialLockCount + 1))
				Expect(lockManager.LockArgsForCall(initialLockCount)).To(Equal(gardenStoreGuid))

				Expect(lockManager.UnlockCallCount()).To(Equal(initialUnlockCount + 1))
				Expect(lockManager.UnlockArgsForCall(initialUnlockCount)).To(Equal(gardenStoreGuid))
			})

			Context("when container exists in garden store", func() {
				It("should return the container", func() {
					container, err := depotClient.GetContainer(gardenStoreGuid)
					Expect(err).NotTo(HaveOccurred())
					Expect(container.Guid).To(Equal(gardenStoreGuid))
					Eventually(gardenStore.LookupCallCount).Should(Equal(1))
				})
			})

			Context("when container does not exists in allocation or garden store", func() {
				BeforeEach(func() {
					gardenStore.LookupReturns(executor.Container{}, executor.ErrContainerNotFound)
				})

				It("should return an error", func() {
					container, err := depotClient.GetContainer("does-not-exist")
					Expect(err).To(HaveOccurred())
					Expect(err).To(Equal(executor.ErrContainerNotFound))
					Expect(container).To(Equal(executor.Container{}))
				})
			})
		})
	})

	Describe("RemainingResources", func() {
		var requests []executor.AllocationRequest
		BeforeEach(func() {
			requests = []executor.AllocationRequest{
				newAllocationRequest("guid-1", defaultMemoryMB, defaultDiskMB),
				newAllocationRequest("guid-2", defaultMemoryMB, defaultDiskMB),
				newAllocationRequest("guid-3", defaultMemoryMB, defaultDiskMB),
			}
		})

		Context("when no containers are running or allocated", func() {
			It("should return the total resources", func() {
				Expect(depotClient.RemainingResources()).To(Equal(resources))
			})
		})

		Context("when some containers are running", func() {
			BeforeEach(func() {
				running := requests[0]
				errMessageMap, err := depotClient.AllocateContainers(requests[1:])
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				gardenStore.ListReturns([]executor.Container{
					newRunningContainer(newRunRequest(running.Guid), running.Resource),
				}, nil)
			})

			It("should reduce resources used by allocated and running containers", func() {
				Expect(depotClient.RemainingResources()).To(Equal(executor.ExecutorResources{
					MemoryMB:   256,
					DiskMB:     256,
					Containers: 0,
				}))
			})
		})

		Context("when some allocated containers are deallocated", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(requests)
				Expect(err).NotTo(HaveOccurred())
				Expect(errMessageMap).To(BeEmpty())

				err = depotClient.DeleteContainer("guid-1")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should make the resources used by the deallocated container available", func() {
				Expect(depotClient.RemainingResources()).To(Equal(executor.ExecutorResources{
					MemoryMB:   512,
					DiskMB:     512,
					Containers: 1,
				}))
			})
		})
	})

	Describe("RemainingResourcesFrom", func() {
		It("returns the available resources from a container snapshot", func() {
			containers := []executor.Container{
				newRunningContainer(newRunRequest("guid-1"), executor.Resource{
					MemoryMB: defaultMemoryMB,
					DiskMB:   defaultDiskMB,
				}),
			}

			execResources, err := depotClient.RemainingResourcesFrom(containers)
			Expect(err).NotTo(HaveOccurred())

			Expect(resources.Subtract(&containers[0].Resource)).To(BeTrue())
			Expect(execResources).To(Equal(resources))
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
