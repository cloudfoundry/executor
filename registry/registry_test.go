package registry_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	. "github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Registry", func() {
	var registry Registry
	var initialCapacity Capacity
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		initialCapacity = Capacity{
			MemoryMB:   100,
			DiskMB:     200,
			Containers: 13,
		}

		timeProvider = faketimeprovider.New(time.Now())
		registry = New(initialCapacity, timeProvider)
	})

	Describe("TotalCapacity", func() {
		It("should always report the initial capacity", func() {
			Ω(registry.TotalCapacity()).Should(Equal(initialCapacity))

			registry.Reserve("a-container", api.ContainerAllocationRequest{
				MemoryMB: 10,
				DiskMB:   20,
			})

			registry.Reserve("another-container", api.ContainerAllocationRequest{
				MemoryMB: 30,
				DiskMB:   70,
			})

			Ω(registry.TotalCapacity()).Should(Equal(initialCapacity))
		})
	})

	Describe("CurrentCapacity", func() {
		It("should always report the available capacity", func() {
			Ω(registry.CurrentCapacity()).Should(Equal(initialCapacity))

			registry.Reserve("a-container", api.ContainerAllocationRequest{
				MemoryMB: 10,
				DiskMB:   20,
			})

			registry.Reserve("another-container", api.ContainerAllocationRequest{
				MemoryMB: 30,
				DiskMB:   70,
			})

			Ω(registry.CurrentCapacity()).Should(Equal(Capacity{
				MemoryMB:   60,
				DiskMB:     110,
				Containers: 11,
			}))
		})
	})

	Describe("Finding by Guid", func() {
		Context("when a container has been allocated", func() {
			BeforeEach(func() {
				registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 10,
					DiskMB:   20,
				})
			})

			It("can find container by GUID", func() {
				container, err := registry.FindByGuid("a-container")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container).ShouldNot(BeZero())
				Ω(container.Guid).Should(Equal("a-container"))
				Ω(container.MemoryMB).Should(Equal(10))
				Ω(container.DiskMB).Should(Equal(20))
			})
		})

		Context("when the requested GUID does not exist", func() {
			It("should return an ErrContainerNotFound error", func() {
				container, err := registry.FindByGuid("nope")
				Ω(container).Should(BeZero())
				Ω(err).Should(MatchError(ErrContainerNotFound))
			})
		})
	})

	Describe("Get all containers", func() {
		Context("when there are no containers", func() {
			It("should return an empty array", func() {
				Ω(registry.GetAllContainers()).Should(BeEmpty())
			})
		})

		Context("when there are containers", func() {
			var containerA, containerB api.Container
			BeforeEach(func() {
				containerA, _ = registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 10,
					DiskMB:   20,
				})

				containerB, _ = registry.Reserve("another-container", api.ContainerAllocationRequest{
					MemoryMB: 10,
					DiskMB:   20,
				})
			})

			It("should return them all", func() {
				containers := registry.GetAllContainers()
				Ω(containers).Should(HaveLen(2))
				Ω(containers).Should(ContainElement(containerA))
				Ω(containers).Should(ContainElement(containerB))
			})
		})
	})

	Describe("Reserving a container", func() {
		var container api.Container
		BeforeEach(func() {
			var err error
			container, err = registry.Reserve("a-container", api.ContainerAllocationRequest{
				MemoryMB: 50,
				DiskMB:   100,
			})
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should return a correctly configured container in the reserved state", func() {
			Ω(container.Guid).Should(Equal("a-container"))
			Ω(container.MemoryMB).Should(Equal(50))
			Ω(container.DiskMB).Should(Equal(100))
			Ω(container.State).Should(Equal(api.StateReserved))
			Ω(container.AllocatedAt).Should(Equal(timeProvider.Time().UnixNano()))
		})

		Context("when reusing an existing guid", func() {
			It("should return an ErrContainerAlreadyExists error", func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 10,
					DiskMB:   20,
				})
				Ω(err).Should(MatchError(ErrContainerAlreadyExists))
			})
		})

		Context("when there is no capacity to reserve the container", func() {
			It("should return an ErrOutOfMemory when out of memory", func() {
				_, err := registry.Reserve("another-container", api.ContainerAllocationRequest{
					MemoryMB: 51,
				})
				Ω(err).Should(MatchError(ErrOutOfMemory))
			})

			It("should return an ErrOutOfDisk when out of disk", func() {
				_, err := registry.Reserve("another-container", api.ContainerAllocationRequest{
					DiskMB: 101,
				})
				Ω(err).Should(MatchError(ErrOutOfDisk))
			})

			It("should return an ErrOutOfContainers when out of containers", func() {
				for i := 0; i < 12; i++ {
					_, err := registry.Reserve(fmt.Sprintf("another-container-%d", i), api.ContainerAllocationRequest{
						MemoryMB: 1,
						DiskMB:   1,
					})
					Ω(err).ShouldNot(HaveOccurred())
				}

				_, err := registry.Reserve("one-too-many-containers", api.ContainerAllocationRequest{
					MemoryMB: 1,
					DiskMB:   1,
				})
				Ω(err).Should(MatchError(ErrOutOfContainers))
			})
		})
	})

	Describe("initializing a container", func() {
		Context("when the container is reserved but not initialized", func() {
			var container api.Container

			BeforeEach(func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 50,
					DiskMB:   100,
				})
				Ω(err).ShouldNot(HaveOccurred())

				container, err = registry.Initialize("a-container")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should transition the container to the initializing state ", func() {
				Ω(container.State).Should(Equal(api.StateInitializing))
			})

			It("should still have the allocation information", func() {
				Ω(container.Guid).Should(Equal("a-container"))
				Ω(container.MemoryMB).Should(Equal(50))
				Ω(container.DiskMB).Should(Equal(100))
				Ω(container.AllocatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})
	})

	Describe("creating a container", func() {
		Context("when the container exists and is initializing", func() {
			var container api.Container

			BeforeEach(func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 50,
					DiskMB:   100,
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = registry.Initialize("a-container")
				Ω(err).ShouldNot(HaveOccurred())

				container, err = registry.Create("a-container", "handle", api.ContainerInitializationRequest{
					CpuPercent: 0.5,
					Ports:      []api.PortMapping{{ContainerPort: 8080}},
					Log:        api.LogConfig{Guid: "log-guid"},
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should transition the container to the created state and attach the passed in handle", func() {
				Ω(container.State).Should(Equal(api.StateCreated))
				Ω(container.ContainerHandle).Should(Equal("handle"))
				Ω(container.CpuPercent).Should(Equal(0.5))
				Ω(container.Ports[0].ContainerPort).Should(Equal(uint32(8080)))
				Ω(container.Log.Guid).Should(Equal("log-guid"))
			})
		})

		Context("when the container does not exist", func() {
			It("should return an ErrContainerNotFound", func() {
				_, err := registry.Create("a-container", "handle", api.ContainerInitializationRequest{})
				Ω(err).Should(MatchError(ErrContainerNotFound))
			})
		})

		Context("when the container is reserved but not initialized", func() {
			BeforeEach(func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 50,
					DiskMB:   100,
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should return an ErrContainerNotInitialized", func() {
				_, err := registry.Create("a-container", "another-handle", api.ContainerInitializationRequest{})
				Ω(err).Should(MatchError(ErrContainerNotInitialized))
			})
		})

		Context("when the container has already been created", func() {
			BeforeEach(func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 50,
					DiskMB:   100,
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = registry.Initialize("a-container")
				Ω(err).ShouldNot(HaveOccurred())

				_, err = registry.Create("a-container", "handle", api.ContainerInitializationRequest{})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should return an ErrContainerNotInitialized", func() {
				_, err := registry.Create("a-container", "another-handle", api.ContainerInitializationRequest{})
				Ω(err).Should(MatchError(ErrContainerNotInitialized))
			})
		})
	})

	Describe("deleting a container", func() {
		var deleteErr error

		JustBeforeEach(func() {
			deleteErr = registry.Delete("a-container")
		})

		Context("when the container exists", func() {
			BeforeEach(func() {
				_, err := registry.Reserve("a-container", api.ContainerAllocationRequest{
					MemoryMB: 50,
					DiskMB:   100,
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not error", func() {
				Ω(deleteErr).ShouldNot(HaveOccurred())
			})

			It("should free its resources", func() {
				Ω(registry.CurrentCapacity()).Should(Equal(registry.TotalCapacity()))
			})

			It("should not be able to find it again", func() {
				_, err := registry.FindByGuid("a-container")
				Ω(err).Should(MatchError(ErrContainerNotFound))
			})
		})

		Context("when the container does not exist", func() {
			It("should error", func() {
				err := registry.Delete("bam")
				Ω(err).Should(MatchError(ErrContainerNotFound))
			})
		})
	})

	Describe("syncing", func() {
		It("removes created containers whose handles are not in the given set", func() {
			reservedContainer := reserveContainer(registry, "guid1")
			initializedContainer := initializeContainer(registry, "guid2")
			createdContainer1 := createContainer(registry, "guid3")
			createdContainer2 := createContainer(registry, "guid4")
			createdContainer3 := createContainer(registry, "guid5")
			completedContainer1 := setupCompletedContainer(registry, "guid8")
			completedContainer2 := setupCompletedContainer(registry, "guid9")

			Ω(getAllGuids(registry)).Should(ConsistOf(
				reservedContainer.Guid,
				initializedContainer.Guid,
				createdContainer1.Guid,
				createdContainer2.Guid,
				createdContainer3.Guid,
				completedContainer1.Guid,
				completedContainer2.Guid,
			))

			registry.Sync(map[string]struct{}{
				createdContainer2.ContainerHandle:   struct{}{},
				completedContainer2.ContainerHandle: struct{}{},
			})

			Ω(getAllGuids(registry)).Should(ConsistOf(
				reservedContainer.Guid,
				initializedContainer.Guid,
				createdContainer2.Guid,
				completedContainer2.Guid,
			))
		})
	})

	It("frees resources for containers whose handles are not in the given set", func() {
		container1 := createContainer(registry, "guid1")
		createContainer(registry, "guid2")

		Ω(registry.CurrentCapacity().Containers).Should(Equal(initialCapacity.Containers - 2))

		registry.Sync(map[string]struct{}{
			container1.ContainerHandle: struct{}{},
		})

		Ω(registry.CurrentCapacity().Containers).Should(Equal(initialCapacity.Containers - 1))
	})
})

func getAllGuids(registry Registry) []string {
	result := []string{}
	for _, container := range registry.GetAllContainers() {
		result = append(result, container.Guid)
	}
	return result
}

func reserveContainer(registry Registry, guid string) api.Container {
	container, err := registry.Reserve(guid, api.ContainerAllocationRequest{})
	Ω(err).ShouldNot(HaveOccurred())
	return container
}

func initializeContainer(registry Registry, guid string) api.Container {
	reserveContainer(registry, guid)

	container, err := registry.Initialize(guid)
	Ω(err).ShouldNot(HaveOccurred())
	return container
}

func createContainer(registry Registry, guid string) api.Container {
	initializeContainer(registry, guid)

	container, err := registry.Create(guid, guid+"-handle", api.ContainerInitializationRequest{})
	Ω(err).ShouldNot(HaveOccurred())
	return container
}

func setupCompletedContainer(registry Registry, guid string) api.Container {
	container := createContainer(registry, guid)

	err := registry.Complete(guid, api.ContainerRunResult{})
	Ω(err).ShouldNot(HaveOccurred())
	return container
}
