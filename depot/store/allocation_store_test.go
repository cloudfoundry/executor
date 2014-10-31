package store_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AllocationStore", func() {
	var (
		timeProvider   *faketimeprovider.FakeTimeProvider
		expirationTime time.Duration

		allocationStore *store.AllocationStore
	)

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Now())
		expirationTime = 1 * time.Second

		allocationStore = store.NewAllocationStore(timeProvider, expirationTime)
	})

	Describe("creating a container", func() {
		var createdContainer executor.Container

		BeforeEach(func() {
			var err error

			createdContainer, err = allocationStore.Create(executor.Container{Guid: "some-guid"})
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("tracks the container's allocated at time", func() {
			Ω(createdContainer.AllocatedAt).Should(Equal(timeProvider.Time().UnixNano()))
		})

		Context("when the expiration time passes", func() {
			It("reaps the reserved container", func() {
				Ω(allocationStore.List()).Should(ContainElement(createdContainer))

				timeProvider.Increment(expirationTime + 1)

				Eventually(allocationStore.List, expirationTime).Should(BeEmpty())
			})
		})

		Context("and then starting to initialize it", func() {
			It("prevents the container from expiring", func() {
				Ω(allocationStore.List()).Should(ContainElement(createdContainer))

				err := allocationStore.StartInitializing(createdContainer.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				timeProvider.Increment(expirationTime + 1)

				initializingContainer := createdContainer
				initializingContainer.State = executor.StateInitializing

				Consistently(allocationStore.List, expirationTime).Should(ContainElement(initializingContainer))
			})
		})

		Context("and then completing it", func() {
			It("prevents the container from expiring", func() {
				Ω(allocationStore.List()).Should(ContainElement(createdContainer))

				runResult := executor.ContainerRunResult{
					Failed:        true,
					FailureReason: "boom",
				}

				err := allocationStore.Complete(createdContainer.Guid, runResult)
				Ω(err).ShouldNot(HaveOccurred())

				timeProvider.Increment(expirationTime + 1)

				completedContainer := createdContainer
				completedContainer.State = executor.StateCompleted
				completedContainer.RunResult = runResult

				Consistently(allocationStore.List, expirationTime).Should(ContainElement(completedContainer))
			})
		})

		Context("when the guid is already taken", func() {
			It("returns an error", func() {
				_, err := allocationStore.Create(createdContainer)
				Ω(err).Should(Equal(executor.ErrContainerGuidNotAvailable))
			})
		})
	})

	Describe("Lookup", func() {
		Context("when the container exists", func() {
			var createdContainer executor.Container

			BeforeEach(func() {
				var err error

				createdContainer, err = allocationStore.Create(executor.Container{
					Guid:  "the-guid",
					State: executor.StateReserved,
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns the container", func() {
				container, err := allocationStore.Lookup("the-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container).Should(Equal(createdContainer))
			})

			Context("and then is destroyed", func() {
				BeforeEach(func() {
					err := allocationStore.Destroy("the-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("returns a container-not-found error", func() {
					_, err := allocationStore.Lookup("the-guid")
					Ω(err).Should(Equal(store.ErrContainerNotFound))
				})
			})
		})

		Context("when the container doesn't exist", func() {
			It("returns a container-not-found error", func() {
				_, err := allocationStore.Lookup("the-guid")
				Ω(err).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("Complete", func() {
		var completeErr error

		JustBeforeEach(func() {
			completeErr = allocationStore.Complete("the-guid", executor.ContainerRunResult{
				Failed:        true,
				FailureReason: "because this is a test",
			})
		})

		Context("when the container exists", func() {
			var createdContainer executor.Container

			BeforeEach(func() {
				var err error

				createdContainer, err = allocationStore.Create(executor.Container{
					Guid:  "the-guid",
					State: executor.StateReserved,
				})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("succeeds", func() {
				Ω(completeErr).ShouldNot(HaveOccurred())
			})

			It("updates the container's state and result", func() {
				container, err := allocationStore.Lookup("the-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(container.State).Should(Equal(executor.StateCompleted))
				Ω(container.RunResult).Should(Equal(executor.ContainerRunResult{
					Failed:        true,
					FailureReason: "because this is a test",
				}))
			})
		})

		Context("when the container doesn't exist", func() {
			It("returns a container-not-found error", func() {
				Ω(completeErr).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("List", func() {
		It("returns all of the containers", func() {
			container1, err := allocationStore.Create(executor.Container{
				Guid:  "guid-1",
				State: executor.StateReserved,
			})
			Ω(err).ShouldNot(HaveOccurred())

			container2, err := allocationStore.Create(executor.Container{
				Guid:  "guid-2",
				State: executor.StateReserved,
			})
			Ω(err).ShouldNot(HaveOccurred())

			Ω(allocationStore.List()).Should(ConsistOf([]executor.Container{
				container1,
				container2,
			}))

			err = allocationStore.Destroy("guid-1")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(allocationStore.List()).Should(ConsistOf([]executor.Container{
				container2,
			}))
		})
	})

	Describe("ConsumedResources", func() {
		It("returns the total resources consumed by all containers", func() {
			_, err := allocationStore.Create(executor.Container{
				Guid:     "guid-1",
				State:    executor.StateReserved,
				MemoryMB: 10,
				DiskMB:   100,
			})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = allocationStore.Create(executor.Container{
				Guid:     "guid-2",
				State:    executor.StateReserved,
				MemoryMB: 5,
				DiskMB:   50,
			})
			Ω(err).ShouldNot(HaveOccurred())

			Ω(allocationStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
				MemoryMB:   15,
				DiskMB:     150,
				Containers: 2,
			}))

			err = allocationStore.Destroy("guid-1")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(allocationStore.ConsumedResources()).Should(Equal(executor.ExecutorResources{
				MemoryMB:   5,
				DiskMB:     50,
				Containers: 1,
			}))
		})
	})

	Describe("Destroy", func() {
		Context("when the container does not exist", func() {
			It("returns a container-not-found error", func() {
				err := allocationStore.Destroy("bogus-guid")
				Ω(err).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})
})
