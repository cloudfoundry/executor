package store_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	"github.com/cloudfoundry-incubator/executor/depot/store/fakes"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AllocationStore", func() {
	var (
		timeProvider   *faketimeprovider.FakeTimeProvider
		expirationTime time.Duration
		tracker        *fakes.FakeAllocationTracker
		emitter        *fakes.FakeEventEmitter
		logger         *lagertest.TestLogger

		allocationStore *store.AllocationStore
	)

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Now())
		expirationTime = 1 * time.Second
		tracker = new(fakes.FakeAllocationTracker)
		emitter = new(fakes.FakeEventEmitter)
		logger = lagertest.NewTestLogger("test")

		allocationStore = store.NewAllocationStore(
			timeProvider,
			expirationTime,
			tracker,
			emitter,
		)
	})

	Describe("creating a container", func() {
		var createdContainer executor.Container

		BeforeEach(func() {
			var err error

			createdContainer, err = allocationStore.Create(executor.Container{Guid: "some-guid"})
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("tracks the container's allocated at time", func() {
			Ω(createdContainer.AllocatedAt).Should(Equal(timeProvider.Now().UnixNano()))
		})

		It("tracks the container's resource usage", func() {
			Ω(tracker.AllocateCallCount()).Should(Equal(1))
			Ω(tracker.AllocateArgsForCall(0)).Should(Equal(createdContainer))
		})

		Context("when the expiration time passes", func() {
			It("reaps the reserved container", func() {
				Ω(allocationStore.List(nil)).Should(ContainElement(createdContainer))

				timeProvider.Increment(expirationTime + 1)

				Eventually(func() interface{} {
					containers, err := allocationStore.List(nil)
					Ω(err).ShouldNot(HaveOccurred())

					return containers
				}, expirationTime).Should(BeEmpty())
			})
		})

		Context("and then starting to initialize it", func() {
			It("prevents the container from expiring", func() {
				Ω(allocationStore.List(nil)).Should(ContainElement(createdContainer))

				err := allocationStore.StartInitializing(createdContainer.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				timeProvider.Increment(expirationTime + 1)

				initializingContainer := createdContainer
				initializingContainer.State = executor.StateInitializing

				Consistently(func() interface{} {
					containers, err := allocationStore.List(nil)
					Ω(err).ShouldNot(HaveOccurred())

					return containers
				}, expirationTime).Should(ContainElement(initializingContainer))
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
					err := allocationStore.Destroy(logger, "the-guid")
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

				err = allocationStore.StartInitializing(createdContainer.Guid)
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

			It("emits a container complete event", func() {
				container, err := allocationStore.Lookup("the-guid")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(emitter.EmitEventCallCount()).Should(Equal(1))
				Ω(emitter.EmitEventArgsForCall(0)).Should(Equal(executor.ContainerCompleteEvent{
					Container: container,
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
		Context("with no tags", func() {
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

				Ω(allocationStore.List(nil)).Should(ConsistOf([]executor.Container{
					container1,
					container2,
				}))

				err = allocationStore.Destroy(logger, "guid-1")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocationStore.List(nil)).Should(ConsistOf([]executor.Container{
					container2,
				}))
			})
		})

		Context("with tags", func() {
			It("returns only containers matching the given tags", func() {
				container1, err := allocationStore.Create(executor.Container{
					Guid:  "guid-1",
					State: executor.StateReserved,
					Tags:  executor.Tags{"a": "b"},
				})
				Ω(err).ShouldNot(HaveOccurred())

				container2, err := allocationStore.Create(executor.Container{
					Guid:  "guid-2",
					State: executor.StateReserved,
					Tags:  executor.Tags{"a": "b", "c": "d"},
				})
				Ω(err).ShouldNot(HaveOccurred())

				container3, err := allocationStore.Create(executor.Container{
					Guid:  "guid-3",
					State: executor.StateReserved,
					Tags:  executor.Tags{"c": "d"},
				})
				Ω(err).ShouldNot(HaveOccurred())

				Ω(allocationStore.List(executor.Tags{"a": "b"})).Should(ConsistOf([]executor.Container{
					container1,
					container2,
				}))

				Ω(allocationStore.List(executor.Tags{"a": "b", "c": "d"})).Should(ConsistOf([]executor.Container{
					container2,
				}))

				Ω(allocationStore.List(executor.Tags{"c": "d"})).Should(ConsistOf([]executor.Container{
					container2,
					container3,
				}))

				Ω(allocationStore.List(executor.Tags{"e": "bogus"})).Should(BeEmpty())
			})
		})
	})

	Describe("Destroy", func() {
		var destroyErr error

		JustBeforeEach(func() {
			destroyErr = allocationStore.Destroy(logger, "the-guid")
		})

		Context("when the container exists", func() {
			BeforeEach(func() {
				_, err := allocationStore.Create(executor.Container{Guid: "the-guid"})
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("succeeds", func() {
				Ω(destroyErr).ShouldNot(HaveOccurred())
			})

			It("releases the container's resource usage", func() {
				Ω(tracker.DeallocateCallCount()).Should(Equal(1))
				Ω(tracker.DeallocateArgsForCall(0)).Should(Equal("the-guid"))
			})
		})

		Context("when the container does not exist", func() {
			It("returns a container-not-found error", func() {
				Ω(destroyErr).Should(Equal(store.ErrContainerNotFound))
			})
		})
	})

	Describe("Transitions", func() {
		expectations := []allocationStoreTransitionExpectation{
			{to: "reserve", from: "non-existent", assertError: "does not occur"},
			{to: "reserve", from: "reserved", assertError: "occurs"},
			{to: "reserve", from: "initializing", assertError: "occurs"},
			{to: "reserve", from: "completed", assertError: "occurs"},

			{to: "initialize", from: "non-existent", assertError: "occurs"},
			{to: "initialize", from: "reserved", assertError: "does not occur"},
			{to: "initialize", from: "initializing", assertError: "occurs"},
			{to: "initialize", from: "completed", assertError: "occurs"},

			{to: "complete", from: "non-existent", assertError: "occurs"},
			{to: "complete", from: "reserved", assertError: "occurs"},
			{to: "complete", from: "initializing", assertError: "does not occur"},
			{to: "complete", from: "completed", assertError: "occurs"},
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

type allocationStoreTransitionExpectation struct {
	from        string
	to          string
	assertError string
}

func (expectation allocationStoreTransitionExpectation) driveFromState(allocationStore *store.AllocationStore, container executor.Container) {
	switch expectation.from {
	case "non-existent":

	case "reserved":
		_, err := allocationStore.Create(container)
		Ω(err).ShouldNot(HaveOccurred())

	case "initializing":
		_, err := allocationStore.Create(container)
		Ω(err).ShouldNot(HaveOccurred())

		err = allocationStore.StartInitializing(container.Guid)
		Ω(err).ShouldNot(HaveOccurred())

	case "completed":
		_, err := allocationStore.Create(container)
		Ω(err).ShouldNot(HaveOccurred())

		err = allocationStore.StartInitializing(container.Guid)
		Ω(err).ShouldNot(HaveOccurred())

		err = allocationStore.Complete(container.Guid, executor.ContainerRunResult{})
		Ω(err).ShouldNot(HaveOccurred())

	default:
		Fail("unknown 'from' state: " + expectation.from)
	}
}

func (expectation allocationStoreTransitionExpectation) transitionToState(allocationStore *store.AllocationStore, container executor.Container) error {
	switch expectation.to {
	case "reserve":
		_, err := allocationStore.Create(container)
		return err

	case "initialize":
		return allocationStore.StartInitializing(container.Guid)

	case "complete":
		return allocationStore.Complete(container.Guid, executor.ContainerRunResult{})

	default:
		Fail("unknown 'to' state: " + expectation.to)
		return nil
	}
}

func (expectation allocationStoreTransitionExpectation) checkErrorResult(err error) {
	switch expectation.assertError {
	case "occurs":
		Ω(err).Should(HaveOccurred())
	case "does not occur":
		Ω(err).ShouldNot(HaveOccurred())
	default:
		Fail("unknown 'assertErr' expectation: " + expectation.assertError)
	}
}
