package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var _ = Describe("RunOnce BBS", func() {
	var bbs *BBS
	var runOnce models.RunOnce

	BeforeEach(func() {
		bbs = New(store)
		runOnce = models.RunOnce{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
			CreatedAt:       time.Now().UnixNano(),
		}
	})

	itRetriesUntilStoreComesBack := func(action func(*BBS, models.RunOnce) error) {
		It("should keep trying until the store comes back", func(done Done) {
			etcdRunner.GoAway()

			runResult := make(chan error)
			go func() {
				err := action(bbs, runOnce)
				runResult <- err
			}()

			time.Sleep(200 * time.Millisecond)

			etcdRunner.ComeBack()

			Ω(<-runResult).ShouldNot(HaveOccurred())

			close(done)
		}, 5)
	}

	Describe("MaintainExecutorPresence", func() {
		var (
			executorId  string
			interval    uint64
			disappeared <-chan bool
			err         error
			presence    PresenceInterface
		)

		BeforeEach(func() {
			executorId = "stubExecutor"
			interval = uint64(1)

			presence, disappeared, err = bbs.MaintainExecutorPresence(interval, executorId)
			Ω(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /executor/EXECUTOR_ID in the store with a TTL", func() {
			node, err := store.Get("/v1/executor/" + executorId)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.Key).Should(Equal("/v1/executor/" + executorId))
			Ω(node.TTL).Should(Equal(interval)) // move to config one day
		})
	})

	Describe("GetAllExecutors", func() {
		It("returns a list of the executor IDs that exist", func() {
			executors, err := bbs.GetAllExecutors()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(executors).Should(BeEmpty())

			presenceA, _, err := bbs.MaintainExecutorPresence(1, "executor-a")
			Ω(err).ShouldNot(HaveOccurred())

			presenceB, _, err := bbs.MaintainExecutorPresence(1, "executor-b")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(func() []string {
				executors, _ := bbs.GetAllExecutors()
				return executors
			}).Should(ContainElement("executor-a"))

			Eventually(func() []string {
				executors, _ := bbs.GetAllExecutors()
				return executors
			}).Should(ContainElement("executor-b"))

			presenceA.Remove()
			presenceB.Remove()
		})
	})

	Describe("ClaimRunOnce", func() {
		Context("when claimed with a correctly configured runOnce", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = "executor-id"
			})

			It("creates /run_once/claimed/<guid>", func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				node, err := store.Get("/v1/run_once/claimed/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/claimed/some-guid",
					Value: runOnce.ToJSON(),
					TTL:   10, // move to config one day
				}))
			})
		})

		Context("when claimed with a runOnce that is missing ExecutorID", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.ClaimRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when the RunOnce is already claimed", func() {
			BeforeEach(func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns an error", func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).ClaimRunOnce)
		})
	})

	Describe("StartRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates running", func() {
			err := bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := store.Get("/v1/run_once/running/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/run_once/running/some-guid",
				Value: runOnce.ToJSON(),
			}))
		})

		Context("when starting with a runOnce that is missing ExecutorID", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.StartRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when starting with a runOnce that is missing ContainerHandle", func() {
			BeforeEach(func() {
				runOnce.ContainerHandle = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.StartRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).StartRunOnce)
		})
	})

	Describe("CompleteRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates an entry under /run_once/completed", func() {
			runOnce.Failed = true
			runOnce.FailureReason = "because i said so"

			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := store.Get("/v1/run_once/completed/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/run_once/completed/some-guid",
				Value: runOnce.ToJSON(),
			}))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).CompleteRunOnce)
		})
	})

	Describe("WatchForDesiredRunOnce", func() {
		var (
			events <-chan (models.RunOnce)
			stop   chan<- bool
		)

		BeforeEach(func() {
			events, stop, _ = bbs.WatchForDesiredRunOnce()
		})

		It("should send an event down the pipe for creates", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should send an event down the pipe for sets", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			e := <-events

			Expect(e).To(Equal(runOnce))

			err = bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			err = bbs.ResolveRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			otherRunOnce := runOnce
			otherRunOnce.Guid = runOnce.Guid + "1"

			err = bbs.DesireRunOnce(otherRunOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherRunOnce))

			close(done)
		})

		It("closes the events channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, ok := <-events

			Expect(ok).To(BeFalse())

			close(done)
		})
	})

	Describe("GetAllPendingRunOnces", func() {
		It("returns all RunOnces in 'pending' state", func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllPendingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllClaimedRunOnces", func() {
		It("returns all RunOnces in 'claimed' state", func() {
			err := bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllClaimedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllStartingRunOnces", func() {
		It("returns all RunOnces in 'running' state", func() {
			err := bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllStartingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllCompletedRunOnces", func() {
		It("returns all RunOnces in 'completed' state", func() {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllCompletedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})
})
