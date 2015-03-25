package depot_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	. "github.com/cloudfoundry-incubator/executor/depot"
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
	var (
		depotClient     executor.Client
		logger          lager.Logger
		fakeClock       *fakeclock.FakeClock
		eventHub        *efakes.FakeHub
		allocationStore AllocationStore
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

		depotClient = NewClientProvider(resources, allocationStore, gardenStore, eventHub, lockManager).WithLogger(logger)
	})

	Describe("AllocateContainers", func() {
		Context("when allocating a single valid container within executor resource limits", func() {
			var containers []executor.Container
			BeforeEach(func() {
				containers = []executor.Container{
					{
						Guid:     "guid-1",
						MemoryMB: 512,
						DiskMB:   512,
					},
				}
			})

			It("should allocate the container", func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				allocatedContainers := allocationStore.List()
				Ω(allocatedContainers).Should(HaveLen(len(containers)))
				Ω(allocatedContainers[0].Guid).Should(Equal("guid-1"))
				Ω(allocatedContainers[0].State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainers[0].CPUWeight).Should(Equal(uint(100)))
			})
		})

		Context("when allocating multiple valid containers", func() {
			var containers []executor.Container

			Context("when total required resources are within executor resource limits", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-2",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-3",
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should allocate all the containers", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(BeEmpty())
					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers)))

					allocatedContainersMap := map[string]*executor.Container{}
					for _, container := range allocatedContainers {
						allocatedContainersMap[container.Guid] = &container
					}

					Ω(allocatedContainersMap).Should(HaveLen(len(containers)))
					Ω(allocatedContainersMap["guid-1"]).ShouldNot(BeNil())
					Ω(allocatedContainersMap["guid-1"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-2"]).ShouldNot(BeNil())
					Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-3"]).ShouldNot(BeNil())
					Ω(allocatedContainersMap["guid-3"].State).Should(Equal(executor.StateReserved))
				})
			})

			Context("when required memory is more than executor resource limits", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 512,
							DiskMB:   256,
						},
						{
							Guid:     "guid-2",
							MemoryMB: 512,
							DiskMB:   256,
						},
						{
							Guid:     "guid-3",
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))

					errMessage, found := errMessageMap["guid-3"]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrInsufficientResourcesAvailable.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)
					Ω(allocatedContainersMap).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainersMap["guid-1"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap).ShouldNot(HaveKey("guid-3"))
				})
			})

			Context("when required disk space is more than executor resource limits", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   512,
						},
						{
							Guid:     "guid-2",
							MemoryMB: 256,
							DiskMB:   512,
						},
						{
							Guid:     "guid-3",
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))
					errMessage, found := errMessageMap["guid-3"]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrInsufficientResourcesAvailable.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)

					Ω(allocatedContainersMap).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainersMap["guid-1"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap).ShouldNot(HaveKey("guid-3"))
				})
			})

			Context("when required number of containers is more than what executor can allocate", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-2",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-3",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-4",
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should allocate first few containers that can fit the available resources", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))
					errMessage, found := errMessageMap["guid-4"]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrInsufficientResourcesAvailable.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))

					allocatedContainersMap := convertSliceToMap(allocatedContainers)
					Ω(allocatedContainersMap).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainersMap["guid-1"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap["guid-3"].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainersMap).ShouldNot(HaveKey("guid-4"))
				})
			})
		})

		Context("when allocating invalid containers list", func() {
			var containers []executor.Container

			Context("when two containers have the same guid", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should not allocate container with duplicate guid", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))
					errMessage, found := errMessageMap["guid-1"]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrContainerGuidNotAvailable.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainers[0].Guid).Should(Equal("guid-1"))
					Ω(allocatedContainers[0].State).Should(Equal(executor.StateReserved))
				})
			})

			Context("when one of the containers has empty guid", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:     "guid-1",
							MemoryMB: 256,
							DiskMB:   256,
						},
						{
							MemoryMB: 256,
							DiskMB:   256,
						},
					}
				})

				It("should not allocate container with empty guid", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))
					errMessage, found := errMessageMap[""]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrGuidNotSpecified.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainers[0].Guid).Should(Equal("guid-1"))
					Ω(allocatedContainers[0].State).Should(Equal(executor.StateReserved))
				})
			})

			Context("when one of the containers has invalid CPUWeight", func() {
				BeforeEach(func() {
					containers = []executor.Container{
						{
							Guid:      "guid-1",
							MemoryMB:  256,
							DiskMB:    256,
							CPUWeight: 150,
						},
						{
							Guid:      "guid-2",
							MemoryMB:  256,
							DiskMB:    256,
							CPUWeight: 80,
						},
					}
				})

				It("should not allocate container with invalid CPUWeight", func() {
					errMessageMap, err := depotClient.AllocateContainers(containers)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(errMessageMap).Should(HaveLen(1))
					errMessage, found := errMessageMap["guid-1"]
					Ω(found).Should(BeTrue())
					Ω(errMessage).Should(Equal(executor.ErrLimitsInvalid.Error()))

					allocatedContainers := allocationStore.List()
					Ω(allocatedContainers).Should(HaveLen(len(containers) - 1))
					Ω(allocatedContainers[0].Guid).Should(Equal("guid-2"))
					Ω(allocatedContainers[0].State).Should(Equal(executor.StateReserved))
					Ω(allocatedContainers[0].CPUWeight).Should(Equal(uint(80)))
				})
			})
		})

		Context("when garden store returns an error", func() {
			var containers []executor.Container
			BeforeEach(func() {
				gardenStore.ListReturns(nil, errors.New("error"))
				containers = []executor.Container{
					{
						Guid:     "guid-1",
						MemoryMB: 256,
						DiskMB:   256,
					},
					{
						Guid:     "guid-2",
						MemoryMB: 256,
						DiskMB:   256,
					},
				}
			})

			It("should return error", func() {
				_, err := depotClient.AllocateContainers(containers)
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrFailureToCheckSpace))
			})
		})
	})

	Describe("RunContainer", func() {
		var (
			containers      []executor.Container
			gardenStoreGuid string
		)

		BeforeEach(func() {
			gardenStoreGuid = "garden-store-guid"

			containers = []executor.Container{
				{
					Guid:     gardenStoreGuid,
					MemoryMB: 512,
					DiskMB:   512,
				},
			}
		})

		JustBeforeEach(func() {
			errMessageMap, err := depotClient.AllocateContainers(containers)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(errMessageMap).Should(BeEmpty())
		})

		Context("when the container is valid", func() {
			It("should create garden container, run it, and remove from allocation store", func() {
				Ω(gardenStore.CreateCallCount()).Should(Equal(0))
				Ω(gardenStore.RunCallCount()).Should(Equal(0))
				Ω(allocationStore.List()).Should(HaveLen(1))
				err := depotClient.RunContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(gardenStore.CreateCallCount).Should(Equal(1))
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
				Eventually(allocationStore.List).Should(BeEmpty())
			})

			It("allocates and drops the container lock", func() {
				initialLockCount := lockManager.LockCallCount()
				initialUnlockCount := lockManager.UnlockCallCount()

				err := depotClient.RunContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(gardenStore.RunCallCount).Should(Equal(1))

				Ω(lockManager.LockCallCount()).Should(Equal(initialLockCount + 1))
				Ω(lockManager.LockArgsForCall(initialLockCount)).Should(Equal(gardenStoreGuid))

				Ω(lockManager.UnlockCallCount()).Should(Equal(initialUnlockCount + 1))
				Ω(lockManager.UnlockArgsForCall(initialUnlockCount)).Should(Equal(gardenStoreGuid))
			})
		})

		Context("when it tries to run a missing container", func() {
			It("should return error", func() {
				err := depotClient.RunContainer("missing-guid")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
			})

			It("does not allocate and drop the container lock", func() {
				initialLockCount := lockManager.LockCallCount()
				initialUnlockCount := lockManager.UnlockCallCount()

				err := depotClient.RunContainer("missing-guid")
				Ω(err).Should(HaveOccurred())

				Ω(lockManager.LockCallCount()).Should(Equal(initialLockCount))
				Ω(lockManager.UnlockCallCount()).Should(Equal(initialUnlockCount))
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
				err := depotClient.RunContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(logger).Should(gbytes.Say("test.depot-client.run-container.container-state-invalid"))
				Ω(gardenStore.CreateCallCount()).Should(Equal(0))
			})
		})

		Context("when garden container creation fails", func() {
			BeforeEach(func() {
				gardenStore.CreateReturns(executor.Container{}, errors.New("some-error"))
			})

			It("should change container's state to failed", func() {
				Ω(gardenStore.CreateCallCount()).Should(Equal(0))
				err := depotClient.RunContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(gardenStore.CreateCallCount).Should(Equal(1))
				Eventually(gardenStore.RunCallCount).Should(Equal(0))
				Eventually(allocationStore.List).Should(HaveLen(1))
				container, err := allocationStore.Lookup(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container.State).Should(Equal(executor.StateCompleted))
				Ω(container.RunResult.Failed).Should(BeTrue())
			})
		})

		Context("when garden container run fails", func() {
			BeforeEach(func() {
				gardenStore.RunReturns(errors.New("some-error"))
			})
			It("should log the error", func() {
				err := depotClient.RunContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
				Eventually(allocationStore.List).Should(BeEmpty())

				Ω(logger).Should(gbytes.Say("test.depot-client.run-container.failed-running-container"))
			})
		})
	})

	Describe("ListContainers", func() {
		var containers []executor.Container

		BeforeEach(func() {
			containers = []executor.Container{
				{
					Guid:     "guid-1",
					MemoryMB: 512,
					DiskMB:   512,
					Tags: executor.Tags{
						"a": "a-value",
						"b": "b-value",
					},
				},
				{
					Guid:     "guid-2",
					MemoryMB: 512,
					DiskMB:   512,
					Tags: executor.Tags{
						"b": "b-value",
						"c": "c-value",
					},
				},
			}
		})

		Context("when containers exist only allocation store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
			})

			It("should return the containers from allocation store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(HaveLen(len(containers)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Ω(returnedContainersMap).Should(HaveLen(len(containers)))
				Ω(returnedContainersMap["guid-1"].State).Should(Equal(executor.StateReserved))
				Ω(returnedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
			})
		})

		Context("when containers exist only garden store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				err = depotClient.RunContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())
				err = depotClient.RunContainer("guid-2")
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(allocationStore.List).Should(HaveLen(0))
				gardenStore.ListReturns(containers, nil)
			})

			It("should return the containers from garden store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(HaveLen(len(containers)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Ω(returnedContainersMap).Should(HaveLen(len(containers)))
				Ω(returnedContainersMap).Should(HaveKey("guid-1"))
				Ω(returnedContainersMap).Should(HaveKey("guid-2"))
			})
		})

		Context("when containers exist in both allocation store and garden store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				err = depotClient.RunContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(allocationStore.List).Should(HaveLen(1))
				gardenStore.ListReturns(containers[:1], nil)
			})

			It("should return the containers from both the stores", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(HaveLen(len(containers)))
				returnedContainersMap := convertSliceToMap(returnedContainers)
				Ω(returnedContainersMap).Should(HaveLen(len(containers)))
				Ω(returnedContainersMap).Should(HaveKey("guid-1"))
				Ω(returnedContainersMap).Should(HaveKey("guid-2"))
			})
		})

		Context("when allocation and garden store are empty", func() {
			It("should return empty list", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(BeEmpty())
			})
		})

		Context("when a duplicate container (same guid) exists in both stores", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())

				err = depotClient.RunContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(allocationStore.List).Should(HaveLen(1))
				gardenStore.ListReturns([]executor.Container{
					{
						Guid:     "guid-1",
						MemoryMB: 512,
						DiskMB:   512,
						State:    executor.StateRunning,
					},
					{
						Guid:     "guid-2",
						MemoryMB: 512,
						DiskMB:   512,
						State:    executor.StateInvalid,
					},
				}, nil)
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
			})

			It("should ignore the duplicate container from garden store", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(HaveLen(len(containers)))

				returnedContainersMap := convertSliceToMap(returnedContainers)
				Ω(returnedContainersMap).Should(HaveLen(len(containers)))
				Ω(returnedContainersMap["guid-1"].State).Should(Equal(executor.StateRunning))
				Ω(returnedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
			})
		})

		Context("when garden store returns an error", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				gardenStore.ListReturns(nil, errors.New("some-error"))
			})

			It("should return an error", func() {
				_, err := depotClient.ListContainers(executor.Tags{})
				Ω(err).Should(HaveOccurred())
				Ω(err.Error()).Should(Equal("some-error"))
			})
		})

		Context("when tags are passed", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
			})

			It("should return the containers matching those tags", func() {
				returnedContainers, err := depotClient.ListContainers(executor.Tags{
					"b": "b-value",
				})
				Ω(err).ShouldNot(HaveOccurred())
				Ω(returnedContainers).Should(HaveLen(len(containers)))
				returnedContainersMap := convertSliceToMap(returnedContainers)
				Ω(returnedContainersMap).Should(HaveLen(len(containers)))
				Ω(returnedContainersMap).Should(HaveKey("guid-1"))
				Ω(returnedContainersMap).Should(HaveKey("guid-2"))
			})

			Context("when non-existent tags are passed", func() {
				It("should return empty list", func() {
					returnedContainers, err := depotClient.ListContainers(executor.Tags{
						"non-existent": "any-value",
					})
					Ω(err).ShouldNot(HaveOccurred())
					Ω(returnedContainers).Should(BeEmpty())
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
			Ω(err).ShouldNot(HaveOccurred())
			Ω(metrics).Should(Equal(expectedMetrics))

			Ω(gardenStore.MetricsCallCount()).Should(Equal(1))

			actualLogger, guids := gardenStore.MetricsArgsForCall(0)
			Ω(actualLogger).ShouldNot(BeNil())
			Ω(guids).Should(ConsistOf("container-guid"))
		})

		Context("when garden fails to get the metrics", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				gardenStore.MetricsReturns(nil, expectedError)
			})

			It("propagates the error", func() {
				_, err := depotClient.GetMetrics("guid-1")
				Ω(err).Should(Equal(expectedError))
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
					executor.Container{Guid: "a-guid", MetricsConfig: executor.MetricsConfig{Guid: "a-metrics"}},
					executor.Container{Guid: "b-guid", MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}},
				}, nil)
			})

			It("gets all the containers", func() {
				Ω(gardenStore.ListCallCount()).Should(Equal(1))
				glogger, tags := gardenStore.ListArgsForCall(0)
				Ω(glogger).ShouldNot(BeNil())
				Ω(tags).Should(BeNil())
			})

			It("retrieves all the metrics", func() {
				Ω(gardenStore.MetricsCallCount()).Should(Equal(1))

				actualLogger, guids := gardenStore.MetricsArgsForCall(0)
				Ω(actualLogger).ShouldNot(BeNil())
				Ω(guids).Should(ConsistOf("a-guid", "b-guid"))
			})

			It("does not error", func() {
				Ω(metricsErr).ShouldNot(HaveOccurred())
			})

			It("returns all the metrics", func() {
				Ω(metrics).Should(HaveLen(2))
				Ω(metrics["a-guid"]).Should(Equal(executor.Metrics{
					MetricsConfig:    executor.MetricsConfig{Guid: "a-metrics"},
					ContainerMetrics: expectedMetrics["a-guid"],
				}))
				Ω(metrics["b-guid"]).Should(Equal(executor.Metrics{
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
				Ω(gardenStore.ListCallCount()).Should(Equal(1))
				_, listTags := gardenStore.ListArgsForCall(0)
				Ω(listTags).Should(Equal(tags))
			})
		})

		Context("containers with missing metric guids", func() {
			BeforeEach(func() {
				tags = nil

				gardenStore.ListReturns([]executor.Container{
					executor.Container{Guid: "a-guid"},
					executor.Container{Guid: "b-guid", MetricsConfig: executor.MetricsConfig{Guid: "b-metrics", Index: 1}},
				}, nil)
			})

			It("retrieves metrics by metric config guids", func() {
				Ω(gardenStore.MetricsCallCount()).Should(Equal(1))

				actualLogger, guids := gardenStore.MetricsArgsForCall(0)
				Ω(actualLogger).ShouldNot(BeNil())
				Ω(guids).Should(ConsistOf("b-guid"))
			})

			It("does not error", func() {
				Ω(metricsErr).ShouldNot(HaveOccurred())
			})

			It("returns the metrics", func() {
				Ω(metrics).Should(HaveLen(1))
				Ω(metrics["b-guid"]).Should(Equal(executor.Metrics{
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
				Ω(metricsErr).Should(Equal(expectedError))
			})
		})

		Context("when garden fails to get the metrics", func() {
			var expectedError error

			BeforeEach(func() {
				expectedError = errors.New("whoops")
				gardenStore.MetricsReturns(nil, expectedError)
			})

			It("propagates the error", func() {
				Ω(metricsErr).Should(Equal(expectedError))
			})
		})
	})

	Describe("DeleteContainer", func() {
		var containers []executor.Container

		BeforeEach(func() {
			containers = []executor.Container{
				{
					Guid:     "guid-1",
					MemoryMB: 256,
					DiskMB:   256,
				},
				{
					Guid:     "guid-2",
					MemoryMB: 256,
					DiskMB:   256,
				},
				{
					Guid:     "guid-3",
					MemoryMB: 256,
					DiskMB:   256,
				},
			}
		})

		It("allocates and drops the container lock", func() {
			depotClient.DeleteContainer("guid-1")

			Ω(lockManager.LockCallCount()).Should(Equal(1))
			Ω(lockManager.LockArgsForCall(0)).Should(Equal("guid-1"))

			Ω(lockManager.UnlockCallCount()).Should(Equal(1))
			Ω(lockManager.UnlockArgsForCall(0)).Should(Equal("guid-1"))
		})

		Context("when container exists in allocation store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				gardenStore.LookupReturns(executor.Container{}, executor.ErrContainerNotFound)
			})

			It("should remove the container from both allocation store, and attempt to remove it from the garden store", func() {
				Ω(gardenStore.DestroyCallCount()).Should(Equal(0))

				err := depotClient.DeleteContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())

				allocatedContainers := allocationStore.List()
				Ω(allocatedContainers).Should(HaveLen(2))
				allocatedContainersMap := convertSliceToMap(allocatedContainers)
				Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainersMap["guid-3"].State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainersMap).ShouldNot(HaveKey("guid-1"))
				Ω(gardenStore.DestroyCallCount()).Should(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Ω(guid).Should(Equal("guid-1"))
			})
		})

		Context("when container exists in garden store", func() {
			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())

				err = depotClient.RunContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(allocationStore.List).Should(HaveLen(2))
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
			})

			It("should remove the container from both allocation store and the garden store", func() {
				Ω(gardenStore.DestroyCallCount()).Should(Equal(0))

				err := depotClient.DeleteContainer("guid-1")
				Ω(err).ShouldNot(HaveOccurred())

				allocatedContainers := allocationStore.List()
				Ω(allocatedContainers).Should(HaveLen(2))
				allocatedContainersMap := convertSliceToMap(allocatedContainers)
				Ω(allocatedContainersMap["guid-2"].State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainersMap["guid-3"].State).Should(Equal(executor.StateReserved))
				Ω(allocatedContainersMap).ShouldNot(HaveKey("guid-1"))
				Ω(gardenStore.DestroyCallCount()).Should(Equal(1))
				Ω(gardenStore.DestroyCallCount()).Should(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Ω(guid).Should(Equal("guid-1"))
			})
		})

		Context("when garden store returns an error", func() {
			BeforeEach(func() {
				gardenStore.DestroyReturns(errors.New("some-error"))
			})

			It("should return an error", func() {
				Ω(gardenStore.DestroyCallCount()).Should(Equal(0))
				err := depotClient.DeleteContainer("guid-1")
				Ω(err).Should(HaveOccurred())
				Ω(gardenStore.DestroyCallCount()).Should(Equal(1))
				_, guid := gardenStore.DestroyArgsForCall(0)
				Ω(guid).Should(Equal("guid-1"))
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
			Ω(lockManager.LockCallCount()).Should(Equal(1))
			Ω(lockManager.LockArgsForCall(0)).Should(Equal(stopGuid))

			Ω(lockManager.UnlockCallCount()).Should(Equal(1))
			Ω(lockManager.UnlockArgsForCall(0)).Should(Equal(stopGuid))
		})

		Context("when the container doesn't exist in the allocation store", func() {
			It("should stop the garden container", func() {
				Ω(stopError).ShouldNot(HaveOccurred())
				Ω(gardenStore.StopCallCount()).Should(Equal(1))
			})
		})

		Context("when the container is in the allocation store", func() {
			var container executor.Container

			BeforeEach(func() {
				container = executor.Container{
					Guid:     stopGuid,
					MemoryMB: 512,
					DiskMB:   512,
				}

				_, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when the container is not completed", func() {
				It("should fail the container", func() {
					result, err := allocationStore.Lookup(stopGuid)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(result.State).Should(Equal(executor.StateCompleted))
					Ω(result.RunResult.Failed).Should(BeTrue())
					Ω(result.RunResult.FailureReason).Should(Equal(ContainerStoppedBeforeRunMessage))
				})
			})

			Context("when the container is completed", func() {
				BeforeEach(func() {
					_, err := allocationStore.Fail(logger, stopGuid, "go away")
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("the run result should remain unchanged", func() {
					result, err := allocationStore.Lookup(stopGuid)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(result.State).Should(Equal(executor.StateCompleted))
					Ω(result.RunResult.Failed).Should(BeTrue())
					Ω(result.RunResult.FailureReason).Should(Equal("go away"))
				})
			})
		})

		Context("when the container is present in gardenStore", func() {
			It("should successfully stop the container", func() {
				Ω(stopError).ShouldNot(HaveOccurred())
				Ω(gardenStore.StopCallCount()).Should(Equal(1))
			})
		})

		Context("when the container is absent in gardenStore", func() {
			BeforeEach(func() {
				gardenStore.StopReturns(errors.New("some-error"))
			})

			It("should fail and return an error", func() {
				Ω(stopError).Should(HaveOccurred())
				Ω(stopError.Error()).Should(Equal("some-error"))
				Ω(gardenStore.StopCallCount()).Should(Equal(1))
			})
		})
	})

	Describe("GetContainer", func() {
		var (
			containers []executor.Container

			gardenStoreGuid     string
			allocationStoreGuid string
		)

		BeforeEach(func() {
			gardenStoreGuid = "garden-store-guid"
			allocationStoreGuid = "allocation-store-guid"
			containers = []executor.Container{
				{
					Guid:     gardenStoreGuid,
					MemoryMB: 512,
					DiskMB:   512,
				},
				{
					Guid:     allocationStoreGuid,
					MemoryMB: 512,
					DiskMB:   512,
				},
			}

			errMessageMap, err := depotClient.AllocateContainers(containers)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(errMessageMap).Should(BeEmpty())
			Ω(gardenStore.CreateCallCount()).Should(Equal(0))
			Ω(gardenStore.RunCallCount()).Should(Equal(0))
			Ω(allocationStore.List()).Should(HaveLen(2))
			err = depotClient.RunContainer(gardenStoreGuid)
			Ω(err).ShouldNot(HaveOccurred())
			Eventually(gardenStore.CreateCallCount).Should(Equal(1))
			Eventually(gardenStore.RunCallCount).Should(Equal(1))
			Eventually(allocationStore.List).Should(HaveLen(1))

			gardenStore.LookupReturns(executor.Container{
				Guid:     gardenStoreGuid,
				MemoryMB: 512,
				DiskMB:   512,
			},
				nil,
			)
		})

		It("allocates and drops the container lock", func() {
			initialLockCount := lockManager.LockCallCount()
			initialUnlockCount := lockManager.UnlockCallCount()

			_, err := depotClient.GetContainer(allocationStoreGuid)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(lockManager.LockCallCount()).Should(Equal(initialLockCount + 1))
			Ω(lockManager.LockArgsForCall(initialLockCount)).Should(Equal(allocationStoreGuid))

			Ω(lockManager.UnlockCallCount()).Should(Equal(initialUnlockCount + 1))
			Ω(lockManager.UnlockArgsForCall(initialUnlockCount)).Should(Equal(allocationStoreGuid))
		})

		Context("when container exists in allocation store", func() {
			It("should return the container", func() {
				container, err := depotClient.GetContainer(allocationStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container.Guid).Should(Equal(allocationStoreGuid))
				Ω(container.State).Should(Equal(executor.StateReserved))
			})
		})

		Context("when container exists in garden store", func() {
			It("should return the container", func() {
				container, err := depotClient.GetContainer(gardenStoreGuid)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container.Guid).Should(Equal(gardenStoreGuid))
				Eventually(gardenStore.CreateCallCount).Should(Equal(1))
				Eventually(gardenStore.RunCallCount).Should(Equal(1))
				Eventually(allocationStore.List).Should(HaveLen(1))
			})
		})

		Context("when container does not exists in allocation or garden store", func() {
			BeforeEach(func() {
				gardenStore.LookupReturns(executor.Container{}, executor.ErrContainerNotFound)
			})
			It("should return an error", func() {
				container, err := depotClient.GetContainer("does-not-exist")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
				Ω(container).Should(Equal(executor.Container{}))
			})
		})
	})

	Describe("RemainingResources", func() {
		var containers []executor.Container
		BeforeEach(func() {
			containers = []executor.Container{
				{
					Guid:     "guid-1",
					MemoryMB: 256,
					DiskMB:   256,
				},
				{
					Guid:     "guid-2",
					MemoryMB: 256,
					DiskMB:   256,
				},
				{
					Guid:     "guid-3",
					MemoryMB: 256,
					DiskMB:   256,
				},
			}
		})

		Context("when no containers are running or allocated", func() {
			It("should return the total resources", func() {
				Ω(depotClient.RemainingResources()).Should(Equal(resources))
			})
		})

		Context("when some containers are allocated", func() {
			expectedResources := executor.ExecutorResources{
				MemoryMB:   256,
				DiskMB:     256,
				Containers: 0,
			}

			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
			})

			Context("when no containers are running", func() {
				It("should reduce resources used by allocated containers", func() {
					Ω(depotClient.RemainingResources()).Should(Equal(expectedResources))
				})
			})

			Context("when some containers are running", func() {
				BeforeEach(func() {
					err := depotClient.RunContainer("guid-1")
					Ω(err).ShouldNot(HaveOccurred())
					Eventually(gardenStore.RunCallCount).Should(Equal(1))
					Eventually(allocationStore.List).Should(HaveLen(2))

					gardenStore.ListReturns(containers[:1], nil)
				})
				It("should reduce resources used by allocated and running containers", func() {
					Ω(depotClient.RemainingResources()).Should(Equal(expectedResources))
				})
			})

			Context("when some allocated containers are deallocated", func() {
				BeforeEach(func() {
					err := depotClient.DeleteContainer("guid-1")
					Ω(err).ShouldNot(HaveOccurred())
				})
				It("should make the resources used by the deallocated container available", func() {
					tmpExpectedResources := executor.ExecutorResources{
						MemoryMB:   512,
						DiskMB:     512,
						Containers: 1,
					}
					Ω(depotClient.RemainingResources()).Should(Equal(tmpExpectedResources))
				})
			})
		})

		Context("when all containers are running", func() {
			expectedResources := executor.ExecutorResources{
				MemoryMB:   256,
				DiskMB:     256,
				Containers: 0,
			}

			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())
				for _, container := range containers {
					err = depotClient.RunContainer(container.Guid)
					Ω(err).ShouldNot(HaveOccurred())
				}
				Eventually(gardenStore.RunCallCount).Should(Equal(3))
				Eventually(allocationStore.List).Should(BeEmpty())

				gardenStore.ListReturns(containers, nil)
			})

			It("should reduce resources used by running containers", func() {
				Ω(depotClient.RemainingResources()).Should(Equal(expectedResources))
			})

			Context("when garden returns error", func() {
				BeforeEach(func() {
					gardenStore.ListReturns(nil, errors.New("some-error"))
				})
				It("should return an error", func() {
					_, err := depotClient.RemainingResources()
					Ω(err).Should(HaveOccurred())
					Ω(err.Error()).Should(Equal("some-error"))
				})
			})

			Context("when some running containers are destroyed", func() {
				BeforeEach(func() {
					gardenStore.ListReturns(containers[:2], nil)
				})
				It("should make the resources used by the destroyed container available", func() {
					tmpExpectedResources := executor.ExecutorResources{
						MemoryMB:   512,
						DiskMB:     512,
						Containers: 1,
					}
					Ω(depotClient.RemainingResources()).Should(Equal(tmpExpectedResources))
				})
			})
		})

		Context("when a container is allocated and running", func() {
			expectedResources := executor.ExecutorResources{
				MemoryMB:   256,
				DiskMB:     256,
				Containers: 0,
			}

			BeforeEach(func() {
				errMessageMap, err := depotClient.AllocateContainers(containers)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(errMessageMap).Should(BeEmpty())

				gardenStore.ListReturns(containers[:1], nil)
			})

			It("should only count the container once", func() {
				Ω(depotClient.RemainingResources()).Should(Equal(expectedResources))
			})
		})
	})

	Describe("TotalResources", func() {
		Context("when asked for total resources", func() {
			It("should return the resources it was configured with", func() {
				Ω(depotClient.TotalResources()).Should(Equal(resources))
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
