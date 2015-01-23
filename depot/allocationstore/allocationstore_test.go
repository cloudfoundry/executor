package allocationstore_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore/fakes"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var logger = lagertest.NewTestLogger("test")

var _ = Describe("Allocation Store", func() {
	var (
		allocationStore  *allocationstore.AllocationStore
		fakeTimeProvider *faketimeprovider.FakeTimeProvider
		fakeEventEmitter *fakes.FakeEventEmitter
		currentTime      time.Time
	)

	BeforeEach(func() {
		currentTime = time.Now()
		fakeTimeProvider = faketimeprovider.New(currentTime)
		fakeEventEmitter = &fakes.FakeEventEmitter{}
		allocationStore = allocationstore.NewAllocationStore(fakeTimeProvider, fakeEventEmitter)
	})

	Describe("List", func() {
		Context("when a container is allocated", func() {
			var container executor.Container

			BeforeEach(func() {
				container = executor.Container{
					Guid:     "banana",
					MemoryMB: 512,
					DiskMB:   512,
				}

				_, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("is included in the list", func() {
				allocations := allocationStore.List()
				Ω(allocations).Should(HaveLen(1))
				Ω(allocations[0].Guid).Should(Equal(container.Guid))
			})

			Context("and then deallocated", func() {
				BeforeEach(func() {
					deallocated := allocationStore.Deallocate(logger, container.Guid)
					Ω(deallocated).Should(BeTrue())
				})

				It("is no longer in the list", func() {
					Ω(allocationStore.List()).Should(BeEmpty())
				})
			})
		})

		Context("when multiple containers are allocated", func() {
			It("they are added to the store", func() {
				_, err := allocationStore.Allocate(logger, executor.Container{
					Guid:     "banana-1",
					MemoryMB: 512,
					DiskMB:   512,
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = allocationStore.Allocate(logger, executor.Container{
					Guid:     "banana-2",
					MemoryMB: 512,
					DiskMB:   512,
				})
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocationStore.List()).Should(HaveLen(2))
			})
		})
	})

	Describe("Allocate", func() {
		var container executor.Container
		BeforeEach(func() {
			container = executor.Container{
				Guid:     "banana",
				MemoryMB: 512,
				DiskMB:   512,
			}
		})

		Context("when the guid is available", func() {
			It("it is marked as RESERVED", func() {
				allocation, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocation.Guid).Should(Equal(container.Guid))
				Ω(allocation.State).Should(Equal(executor.StateReserved))
				Ω(allocation.AllocatedAt).Should(Equal(currentTime.UnixNano()))

				Ω(fakeEventEmitter.EmitEventCallCount()).Should(Equal(1))
				Ω(fakeEventEmitter.EmitEventArgsForCall(0)).Should(Equal(executor.NewContainerReservedEvent(allocation)))
			})
		})

		Context("when the guid is not available", func() {
			BeforeEach(func() {
				_, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("errors and does not store the duplicate", func() {
				_, err := allocationStore.Allocate(logger, container)
				Ω(err).Should(HaveOccurred())
				Ω(allocationStore.List()).Should(HaveLen(1))
			})
		})
	})

	Describe("Initialize", func() {
		var container executor.Container
		BeforeEach(func() {
			container = executor.Container{
				Guid:     "banana",
				MemoryMB: 512,
				DiskMB:   512,
			}
			_, err := allocationStore.Allocate(logger, container)
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when the guid is available", func() {
			It("it is marked as INITIALIZING", func() {
				err := allocationStore.Initialize(logger, container.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				allocation, err := allocationStore.Lookup(container.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocation.Guid).Should(Equal(container.Guid))
				Ω(allocation.State).Should(Equal(executor.StateInitializing))
			})
		})

		Context("when the guid is not available", func() {
			It("errors", func() {
				err := allocationStore.Initialize(logger, "doesnt-exist")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Lookup", func() {
		var container executor.Container
		BeforeEach(func() {
			container = executor.Container{
				Guid:     "banana",
				MemoryMB: 512,
				DiskMB:   512,
			}
			_, err := allocationStore.Allocate(logger, container)
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when the guid is available", func() {
			It("it is returns the container", func() {
				allocation, err := allocationStore.Lookup(container.Guid)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(allocation.Guid).Should(Equal(container.Guid))
			})
		})

		Context("when the guid is not available", func() {
			It("errors", func() {
				_, err := allocationStore.Lookup("doesnt-exist")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Fail", func() {
		var container executor.Container
		BeforeEach(func() {
			container = executor.Container{
				Guid:     "banana",
				MemoryMB: 512,
				DiskMB:   512,
			}
		})

		Context("when the container is not in the allocation store", func() {
			It("errors", func() {
				_, err := allocationStore.Fail(logger, container.Guid, "failure-response")
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(executor.ErrContainerNotFound))

				Ω(fakeEventEmitter.EmitEventCallCount()).Should(Equal(0))
			})
		})

		Context("when the container is in the allocation store", func() {
			BeforeEach(func() {
				_, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("it is marked as COMPLETED with failure reason", func() {
				emitCallCount := fakeEventEmitter.EmitEventCallCount()
				allocation, err := allocationStore.Fail(logger, container.Guid, "failure-reason")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocation.Guid).Should(Equal(container.Guid))
				Ω(allocation.State).Should(Equal(executor.StateCompleted))
				Ω(allocation.RunResult).Should(Equal(executor.ContainerRunResult{
					Failed:        true,
					FailureReason: "failure-reason",
				}))

				Ω(fakeEventEmitter.EmitEventCallCount()).Should(Equal(emitCallCount + 1))
				Ω(fakeEventEmitter.EmitEventArgsForCall(emitCallCount)).Should(Equal(executor.NewContainerCompleteEvent(allocation)))
			})

			It("remains in the allocation store as reserved", func() {
				c, err := allocationStore.Lookup(container.Guid)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(c.State).Should(Equal(executor.StateReserved))
			})

			Context("when the container is already in the completed state", func() {
				BeforeEach(func() {
					err := allocationStore.Initialize(logger, container.Guid)
					Ω(err).ShouldNot(HaveOccurred())

					_, err = allocationStore.Fail(logger, container.Guid, "force-completed")
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("remains in the allocation store as completed", func() {
					c, err := allocationStore.Lookup(container.Guid)
					Ω(err).ShouldNot(HaveOccurred())
					Ω(c.State).Should(Equal(executor.StateCompleted))
				})

				It("fails with an invalid transition error", func() {
					expectedEmitEventCount := fakeEventEmitter.EmitEventCallCount()

					_, err := allocationStore.Fail(logger, container.Guid, "already-completed")
					Ω(err).Should(Equal(executor.ErrInvalidTransition))

					Ω(fakeEventEmitter.EmitEventCallCount()).Should(Equal(expectedEmitEventCount))
				})
			})
		})
	})

	Describe("Deallocate", func() {
		var container executor.Container

		BeforeEach(func() {
			container = executor.Container{
				Guid:     "banana",
				MemoryMB: 512,
				DiskMB:   512,
			}
		})

		Context("when the guid is in the list", func() {
			BeforeEach(func() {
				_, err := allocationStore.Allocate(logger, container)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("it is removed from the list, and returns true", func() {
				count := len(allocationStore.List())

				deallocated := allocationStore.Deallocate(logger, container.Guid)
				Ω(deallocated).Should(BeTrue())

				Ω(allocationStore.List()).Should(HaveLen(count - 1))
			})
		})

		Context("when the guid is not in the list", func() {
			It("returns false", func() {
				deallocated := allocationStore.Deallocate(logger, "doesnt-exist")
				Ω(deallocated).Should(BeFalse())
			})
		})
	})

	Describe("Registry Pruner", func() {
		var (
			expirationTime time.Duration
			process        ifrit.Process
		)

		BeforeEach(func() {
			_, err := allocationStore.Allocate(logger, executor.Container{
				Guid:     "forever-reserved",
				MemoryMB: 512,
				DiskMB:   512,
			})
			Ω(err).ShouldNot(HaveOccurred())

			_, err = allocationStore.Allocate(logger, executor.Container{
				Guid:     "eventually-initialized",
				MemoryMB: 512,
				DiskMB:   512,
			})
			Ω(err).ShouldNot(HaveOccurred())

			err = allocationStore.Initialize(logger, "eventually-initialized")
			Ω(err).ShouldNot(HaveOccurred())

			expirationTime = 20 * time.Millisecond

			pruner := allocationStore.RegistryPruner(logger, expirationTime)
			process = ginkgomon.Invoke(pruner)
		})

		AfterEach(func() {
			ginkgomon.Interrupt(process)
		})

		Context("when the elapsed time is less than expiration period", func() {
			BeforeEach(func() {
				fakeTimeProvider.Increment(expirationTime / 2)
			})

			It("all containers are still in the list", func() {
				Consistently(allocationStore.List).Should(HaveLen(2))
			})
		})

		Context("when the elapsed time is more than expiration period", func() {
			BeforeEach(func() {
				fakeTimeProvider.Increment(2 * expirationTime)
			})

			It("it removes only RESERVED containers from the list", func() {
				Eventually(allocationStore.List).Should(HaveLen(1))
				Ω(allocationStore.List()[0].Guid).Should(Equal("eventually-initialized"))
			})
		})
	})

	Describe("Transitions", func() {
		expectations := []transitionExpectation{
			{to: "reserve", from: "non-existent", assertError: "does not occur"},
			{to: "reserve", from: "reserved", assertError: "occurs"},
			{to: "reserve", from: "initializing", assertError: "occurs"},
			{to: "reserve", from: "failed", assertError: "occurs"},

			{to: "initialize", from: "non-existent", assertError: "occurs"},
			{to: "initialize", from: "reserved", assertError: "does not occur"},
			{to: "initialize", from: "initializing", assertError: "occurs"},
			{to: "initialize", from: "failed", assertError: "occurs"},

			{to: "fail", from: "non-existent", assertError: "occurs"},
			{to: "fail", from: "reserved", assertError: "does not occur"},
			{to: "fail", from: "initializing", assertError: "does not occur"},
			{to: "fail", from: "failed", assertError: "occurs"},
		}

		for _, expectation := range expectations {
			expectation := expectation
			It("error "+expectation.assertError+" when transitioning from "+expectation.from+" to "+expectation.to, func() {
				container := executor.Container{Guid: "some-guid"}
				expectation.driveFromState(allocationStore, container)
				err := expectation.transitionToState(allocationStore, container)
				expectation.checkErrorResult(err)
			})
		}
	})
})

type transitionExpectation struct {
	from        string
	to          string
	assertError string
}

func (expectation transitionExpectation) driveFromState(allocationStore *allocationstore.AllocationStore, container executor.Container) {
	switch expectation.from {
	case "non-existent":

	case "reserved":
		_, err := allocationStore.Allocate(logger, container)
		Ω(err).ShouldNot(HaveOccurred())

	case "initializing":
		_, err := allocationStore.Allocate(logger, container)
		Ω(err).ShouldNot(HaveOccurred())

		err = allocationStore.Initialize(logger, container.Guid)
		Ω(err).ShouldNot(HaveOccurred())

	case "failed":
		_, err := allocationStore.Allocate(logger, container)
		Ω(err).ShouldNot(HaveOccurred())

		err = allocationStore.Initialize(logger, container.Guid)
		Ω(err).ShouldNot(HaveOccurred())

		_, err = allocationStore.Fail(logger, container.Guid, "failure-reason")
		Ω(err).ShouldNot(HaveOccurred())

	default:
		Fail("unknown 'from' state: " + expectation.from)
	}
}

func (expectation transitionExpectation) transitionToState(allocationStore *allocationstore.AllocationStore, container executor.Container) error {
	switch expectation.to {
	case "reserve":
		_, err := allocationStore.Allocate(logger, container)
		return err

	case "initialize":
		return allocationStore.Initialize(logger, container.Guid)

	case "fail":
		_, err := allocationStore.Fail(logger, container.Guid, "failure-reason")
		return err

	default:
		Fail("unknown 'to' state: " + expectation.to)
		return nil
	}
}

func (expectation transitionExpectation) checkErrorResult(err error) {
	switch expectation.assertError {
	case "occurs":
		Ω(err).Should(HaveOccurred())
	case "does not occur":
		Ω(err).ShouldNot(HaveOccurred())
	default:
		Fail("unknown 'assertErr' expectation: " + expectation.assertError)
	}
}
