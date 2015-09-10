package allocationstore_test

import (
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore/fakes"
	"github.com/pivotal-golang/clock/fakeclock"
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
		fakeClock        *fakeclock.FakeClock
		fakeEventEmitter *fakes.FakeEventEmitter
		currentTime      time.Time
	)

	BeforeEach(func() {
		currentTime = time.Now()
		fakeClock = fakeclock.NewFakeClock(currentTime)
		fakeEventEmitter = &fakes.FakeEventEmitter{}
		allocationStore = allocationstore.NewAllocationStore(fakeClock, fakeEventEmitter)
	})

	Describe("List", func() {
		Context("when a container is allocated", func() {
			var request *executor.AllocationRequest

			BeforeEach(func() {
				request = newAllocationRequest("banana", 512, 512)

				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())
			})

			It("is included in the list", func() {
				allocations := allocationStore.List()
				Expect(allocations).To(HaveLen(1))
				Expect(allocations[0].Guid).To(Equal(request.Guid))
				Expect(allocations[0].Resource).To(BeEquivalentTo(request.Resource))
			})

			Context("and then deallocated", func() {
				BeforeEach(func() {
					deallocated := allocationStore.Deallocate(logger, request.Guid)
					Expect(deallocated).To(BeTrue())
				})

				It("is no longer in the list", func() {
					Expect(allocationStore.List()).To(BeEmpty())
				})
			})
		})

		Context("when multiple containers are allocated", func() {
			It("they are added to the store", func() {
				_, err := allocationStore.Allocate(logger, newAllocationRequest("banana", 512, 512))
				Expect(err).NotTo(HaveOccurred())

				_, err = allocationStore.Allocate(logger, newAllocationRequest("banana-2", 512, 512))
				Expect(err).NotTo(HaveOccurred())

				Expect(allocationStore.List()).To(HaveLen(2))
			})
		})
	})

	Describe("Allocate", func() {
		Context("when the guid is available", func() {
			It("it is marked as RESERVED", func() {
				request := newAllocationRequest("banana", 512, 512)

				allocation, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())

				Expect(allocation.Guid).To(Equal(request.Guid))
				Expect(allocation.Resource).To(BeEquivalentTo(request.Resource))
				Expect(allocation.AllocatedAt).To(Equal(currentTime.UnixNano()))

				Expect(allocation.State).To(Equal(executor.StateReserved))

				Expect(fakeEventEmitter.EmitCallCount()).To(Equal(1))
				Expect(fakeEventEmitter.EmitArgsForCall(0)).To(BeEquivalentTo(executor.NewContainerReservedEvent(allocation)))
			})
		})

		Context("when the guid is not available", func() {
			It("errors and does not store the duplicate", func() {
				request := newAllocationRequest("banana", 512, 512)

				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())

				_, err = allocationStore.Allocate(logger, request)
				Expect(err).To(HaveOccurred())

				Expect(allocationStore.List()).To(HaveLen(1))
			})
		})
	})

	Describe("Initialize", func() {
		Context("when the guid is available", func() {
			const guid = "banana"

			BeforeEach(func() {
				request := newAllocationRequest(guid, 512, 512)
				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())
			})

			It("it is marked as INITIALIZING", func() {
				runReq := newRunRequest(guid)
				err := allocationStore.Initialize(logger, runReq)
				Expect(err).NotTo(HaveOccurred())

				container, err := allocationStore.Lookup(guid)
				Expect(err).NotTo(HaveOccurred())

				Expect(container.Guid).To(Equal(guid))
				Expect(container.RunInfo).To(BeEquivalentTo(runReq.RunInfo))
				Expect(container.State).To(Equal(executor.StateInitializing))
			})
		})

		Context("when the guid is not available", func() {
			It("errors", func() {
				runReq := newRunRequest("doesnt-exist")
				err := allocationStore.Initialize(logger, runReq)
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Lookup", func() {
		Context("when the guid is available", func() {
			const guid = "banana"

			BeforeEach(func() {
				request := newAllocationRequest(guid, 512, 512)
				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())
			})

			It("it is returns the container", func() {
				allocation, err := allocationStore.Lookup(guid)
				Expect(err).NotTo(HaveOccurred())
				Expect(allocation.Guid).To(Equal(guid))
			})
		})

		Context("when the guid is not available", func() {
			It("errors", func() {
				_, err := allocationStore.Lookup("doesnt-exist")
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrContainerNotFound))
			})
		})
	})

	Describe("Fail", func() {
		Context("when the container is not in the allocation store", func() {
			It("errors", func() {
				_, err := allocationStore.Fail(logger, "doesnt-exist", "failure-response")
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(executor.ErrContainerNotFound))

				Expect(fakeEventEmitter.EmitCallCount()).To(Equal(0))
			})
		})

		Context("when the container is in the allocation store", func() {
			const guid = "banana"

			BeforeEach(func() {
				request := newAllocationRequest(guid, 512, 512)
				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())
			})

			It("it is marked as COMPLETED with failure reason", func() {
				emitCallCount := fakeEventEmitter.EmitCallCount()
				allocation, err := allocationStore.Fail(logger, guid, "failure-reason")
				Expect(err).NotTo(HaveOccurred())

				Expect(allocation.Guid).To(Equal(guid))
				Expect(allocation.State).To(Equal(executor.StateCompleted))
				Expect(allocation.RunResult).To(Equal(executor.ContainerRunResult{
					Failed:        true,
					FailureReason: "failure-reason",
				}))

				Expect(fakeEventEmitter.EmitCallCount()).To(Equal(emitCallCount + 1))
				Expect(fakeEventEmitter.EmitArgsForCall(emitCallCount)).To(Equal(executor.NewContainerCompleteEvent(allocation)))
			})

			It("remains in the allocation store as reserved", func() {
				c, err := allocationStore.Lookup(guid)
				Expect(err).NotTo(HaveOccurred())
				Expect(c.State).To(Equal(executor.StateReserved))
			})

			Context("when the container is already in the completed state", func() {
				BeforeEach(func() {
					runReq := newRunRequest(guid)
					err := allocationStore.Initialize(logger, runReq)
					Expect(err).NotTo(HaveOccurred())

					_, err = allocationStore.Fail(logger, guid, "force-completed")
					Expect(err).NotTo(HaveOccurred())
				})

				It("remains in the allocation store as completed", func() {
					c, err := allocationStore.Lookup(guid)
					Expect(err).NotTo(HaveOccurred())
					Expect(c.State).To(Equal(executor.StateCompleted))
				})

				It("fails with an invalid transition error", func() {
					expectedEmitCount := fakeEventEmitter.EmitCallCount()

					_, err := allocationStore.Fail(logger, guid, "already-completed")
					Expect(err).To(Equal(executor.ErrInvalidTransition))

					Expect(fakeEventEmitter.EmitCallCount()).To(Equal(expectedEmitCount))
				})
			})
		})
	})

	Describe("Deallocate", func() {
		Context("when the guid is in the list", func() {
			const guid = "banana"

			BeforeEach(func() {
				request := newAllocationRequest(guid, 512, 512)
				_, err := allocationStore.Allocate(logger, request)
				Expect(err).NotTo(HaveOccurred())
			})

			It("it is removed from the list, and returns true", func() {
				count := len(allocationStore.List())

				deallocated := allocationStore.Deallocate(logger, guid)
				Expect(deallocated).To(BeTrue())

				Expect(allocationStore.List()).To(HaveLen(count - 1))
			})
		})

		Context("when the guid is not in the list", func() {
			It("returns false", func() {
				deallocated := allocationStore.Deallocate(logger, "doesnt-exist")
				Expect(deallocated).To(BeFalse())
			})
		})
	})

	Describe("Registry Pruner", func() {
		var (
			expirationTime time.Duration
			process        ifrit.Process
		)

		BeforeEach(func() {
			_, err := allocationStore.Allocate(logger, newAllocationRequest("forever-reserved", 512, 512))
			Expect(err).NotTo(HaveOccurred())

			_, err = allocationStore.Allocate(logger, newAllocationRequest("eventually-initialized", 512, 512))
			Expect(err).NotTo(HaveOccurred())

			err = allocationStore.Initialize(logger, newRunRequest("eventually-initialized"))
			Expect(err).NotTo(HaveOccurred())

			expirationTime = 20 * time.Millisecond

			pruner := allocationStore.RegistryPruner(logger, expirationTime)
			process = ginkgomon.Invoke(pruner)
		})

		AfterEach(func() {
			ginkgomon.Interrupt(process)
		})

		Context("when the elapsed time is less than expiration period", func() {
			BeforeEach(func() {
				fakeClock.Increment(expirationTime / 2)
			})

			It("all containers are still in the list", func() {
				Consistently(allocationStore.List).Should(HaveLen(2))
			})
		})

		Context("when the elapsed time is more than expiration period", func() {
			BeforeEach(func() {
				fakeClock.Increment(2 * expirationTime)
			})

			It("it removes only RESERVED containers from the list", func() {
				Eventually(allocationStore.List).Should(HaveLen(1))
				Expect(allocationStore.List()[0].Guid).To(Equal("eventually-initialized"))
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
	allocReq := executor.NewAllocationRequest(container.Guid,&container.Resource,container.Tags)
	runReq := executor.NewRunRequest(container.Guid,&container.RunInfo,container.Tags)

	switch expectation.from {
	case "non-existent":

	case "reserved":
		_, err := allocationStore.Allocate(logger, &allocReq)
		Expect(err).NotTo(HaveOccurred())

	case "initializing":
		_, err := allocationStore.Allocate(logger, &allocReq)
		Expect(err).NotTo(HaveOccurred())


		err = allocationStore.Initialize(logger, &runReq)
		Expect(err).NotTo(HaveOccurred())

	case "failed":
		_, err := allocationStore.Allocate(logger, &allocReq)
		Expect(err).NotTo(HaveOccurred())

		err = allocationStore.Initialize(logger, &runReq)
		Expect(err).NotTo(HaveOccurred())

		_, err = allocationStore.Fail(logger, container.Guid, "failure-reason")
		Expect(err).NotTo(HaveOccurred())

	default:
		Fail("unknown 'from' state: " + expectation.from)
	}
}

func (expectation transitionExpectation) transitionToState(allocationStore *allocationstore.AllocationStore, container executor.Container) error {
	switch expectation.to {
	case "reserve":
		allocReq := executor.NewAllocationRequest(container.Guid,&container.Resource,container.Tags)
		_, err := allocationStore.Allocate(logger, &allocReq)
		return err

	case "initialize":
		runReq := executor.NewRunRequest(container.Guid,&container.RunInfo,container.Tags)
		return allocationStore.Initialize(logger, &runReq)

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
		Expect(err).To(HaveOccurred())
	case "does not occur":
		Expect(err).NotTo(HaveOccurred())
	default:
		Fail("unknown 'assertErr' expectation: " + expectation.assertError)
	}
}

func newAllocationRequest(guid string, memoryMB, diskMB int, tagses ...executor.Tags) *executor.AllocationRequest {
	resource := executor.NewResource(memoryMB, diskMB, "linux")
	var tags executor.Tags
	if len(tagses) > 0 {
		tags = tagses[0]
	}
	req := executor.NewAllocationRequest(guid, &resource, tags)
	return &req
}

func newRunRequest(guid string) *executor.RunRequest {
	runInfo := executor.RunInfo{
	// TODO: Fill in required fields.
	}
	r := executor.NewRunRequest(guid, &runInfo, nil)
	return &r
}
