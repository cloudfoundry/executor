package bbs_test

import (
	"path"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("Executor BBS", func() {
	var bbs *BBS
	var runOnce *models.RunOnce
	var timeToClaim time.Duration
	var presence PresenceInterface
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeToClaim = 30 * time.Second
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
		runOnce = &models.RunOnce{
			Guid: "some-guid",
		}
	})

	itRetriesUntilStoreComesBack := func(action func() error) {
		It("should keep trying until the store comes back", func(done Done) {
			etcdRunner.GoAway()

			runResult := make(chan error)
			go func() {
				err := action()
				runResult <- err
			}()

			time.Sleep(200 * time.Millisecond)

			etcdRunner.ComeBack()

			Ω(<-runResult).ShouldNot(HaveOccurred())

			close(done)
		}, 5)
	}

	Describe("ClaimRunOnce", func() {
		Context("when claiming a pending RunOnce", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("puts the RunOnce in the claim state", func() {
				err := bbs.ClaimRunOnce(runOnce, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.State).Should(Equal(models.RunOnceStateClaimed))
				Ω(runOnce.ExecutorID).Should(Equal("executor-ID"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: runOnce.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				err := bbs.ClaimRunOnce(runOnce, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ClaimRunOnce(runOnce, "executor-ID")
				})
			})
		})

		Context("when claiming a RunOnce that is not in the pending state", func() {
			It("returns an error", func() {
				err := bbs.ClaimRunOnce(runOnce, "executor-ID")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("StartRunOnce", func() {
		Context("when starting a claimed RunOnce", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the state to running", func() {
				err := bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.State).Should(Equal(models.RunOnceStateRunning))
				Ω(runOnce.ContainerHandle).Should(Equal("container-handle"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: runOnce.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err := bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.StartRunOnce(runOnce, "container-handle")
				})
			})
		})

		Context("When starting a RunOnce that is not in the claimed state", func() {
			It("returns an error", func() {
				err := bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("CompleteRunOnce", func() {
		Context("when completing a running RunOnce", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the RunOnce in the completed state", func() {
				err := bbs.CompleteRunOnce(runOnce, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.Failed).Should(BeTrue())
				Ω(runOnce.FailureReason).Should(Equal("because i said so"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: runOnce.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err := bbs.CompleteRunOnce(runOnce, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.CompleteRunOnce(runOnce, false, "", "a result")
				})
			})
		})

		Context("When completing a RunOnce that is not in the running state", func() {
			It("returns an error", func() {
				err := bbs.CompleteRunOnce(runOnce, true, "because i said so", "a result")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("MaintainExecutorPresence", func() {
		var (
			executorId  string
			interval    time.Duration
			disappeared <-chan bool
			err         error
			presence    PresenceInterface
		)

		BeforeEach(func() {
			executorId = "stubExecutor"
			interval = 1 * time.Second

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
			Ω(node.TTL).Should(Equal(uint64(interval.Seconds()))) // move to config one day
		})
	})

	Describe("GetAllExecutors", func() {
		It("returns a list of the executor IDs that exist", func() {
			executors, err := bbs.GetAllExecutors()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(executors).Should(BeEmpty())

			presenceA, _, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-a")
			Ω(err).ShouldNot(HaveOccurred())

			presenceB, _, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-b")
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

	Describe("WatchForDesiredRunOnce", func() {
		var (
			events <-chan *models.RunOnce
			stop   chan<- bool
			errors <-chan error
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForDesiredRunOnce()
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

		It("closes the events and errors channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())

			close(done)
		})
	})

	Describe("ConvergeRunOnce", func() {
		var desiredEvents <-chan *models.RunOnce
		var completedEvents <-chan *models.RunOnce

		commenceWatching := func() {
			desiredEvents, _, _ = bbs.WatchForDesiredRunOnce()
			completedEvents, _, _ = bbs.WatchForCompletedRunOnce()
		}

		Context("when a RunOnce is malformed", func() {
			It("should delete it", func() {
				nodeKey := path.Join(RunOnceSchemaRoot, "some-guid")

				err := store.Create(storeadapter.StoreNode{
					Key:   nodeKey,
					Value: []byte("ß"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = store.Get(nodeKey)
				Ω(err).ShouldNot(HaveOccurred())

				bbs.ConvergeRunOnce(timeToClaim)

				_, err = store.Get(nodeKey)
				Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when a RunOnce is pending", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should kick the RunOnce", func() {
				timeProvider.IncrementBySeconds(1)
				commenceWatching()
				bbs.ConvergeRunOnce(timeToClaim)

				var noticedOnce *models.RunOnce
				Eventually(desiredEvents).Should(Receive(&noticedOnce))

				runOnce.UpdatedAt = timeProvider.Time().UnixNano()
				Ω(noticedOnce).Should(Equal(runOnce))
			})

			Context("when the RunOnce has been pending for longer than the timeToClaim", func() {
				It("should mark the RunOnce as completed & failed", func() {
					timeProvider.IncrementBySeconds(31)
					commenceWatching()
					bbs.ConvergeRunOnce(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.RunOnce
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("time limit"))
				})
			})
		})

		Context("when a RunOnce is claimed", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				presence, _, err = bbs.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeRunOnce(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been claimed for > 10 seconds", func() {
				It("should mark the RunOnce as pending", func() {
					timeProvider.IncrementBySeconds(10)
					commenceWatching()

					bbs.ConvergeRunOnce(timeToClaim)

					Consistently(completedEvents).ShouldNot(Receive())

					var noticedOnce *models.RunOnce
					Eventually(desiredEvents).Should(Receive(&noticedOnce))

					runOnce.State = models.RunOnceStatePending
					runOnce.UpdatedAt = timeProvider.Time().UnixNano()
					Ω(noticedOnce).Should(Equal(runOnce))
				})
			})

			Context("when the associated executor is missing", func() {
				BeforeEach(func() {
					presence.Remove()
				})

				It("should mark the RunOnce as completed & failed", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeRunOnce(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.RunOnce
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a RunOnce is running", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				presence, _, err = bbs.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeRunOnce(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the associated executor is missing", func() {
				BeforeEach(func() {
					presence.Remove()
				})

				It("should mark the RunOnce as completed & failed", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeRunOnce(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.RunOnce
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a RunOnce is completed", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteRunOnce(runOnce, true, "'cause I said so", "a magical result")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should kick the RunOnce", func() {
				timeProvider.IncrementBySeconds(1)
				commenceWatching()

				bbs.ConvergeRunOnce(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())

				var noticedOnce *models.RunOnce
				Eventually(completedEvents).Should(Receive(&noticedOnce))

				Ω(noticedOnce.Failed).Should(Equal(true))
				Ω(noticedOnce.FailureReason).Should(Equal("'cause I said so"))
				Ω(noticedOnce.Result).Should(Equal("a magical result"))
				Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})

		Context("when a RunOnce is resolving", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimRunOnce(runOnce, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteRunOnce(runOnce, true, "'cause I said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ResolvingRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeRunOnce(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been resolving for > 10 seconds", func() {
				It("should put the RunOnce back into the completed state", func() {
					timeProvider.IncrementBySeconds(10)
					commenceWatching()

					bbs.ConvergeRunOnce(timeToClaim)

					var noticedOnce *models.RunOnce
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					runOnce.State = models.RunOnceStateCompleted
					runOnce.UpdatedAt = timeProvider.Time().UnixNano()
					Ω(noticedOnce).Should(Equal(runOnce))
				})
			})
		})
	})

	Context("MaintainConvergeLock", func() {
		Describe("Maintain the converge lock", func() {

			Context("when the lock is available", func() {
				It("should return immediately", func() {
					lostLock, releaseLock, err := bbs.MaintainConvergeLock(1*time.Minute, "my_id")

					Ω(err).ShouldNot(HaveOccurred())
					Ω(lostLock).ShouldNot(BeNil())
					Ω(releaseLock).ShouldNot(BeNil())
				})

				It("should maintain the lock in the background", func() {
					_, releaseLock, err := bbs.MaintainConvergeLock(1*time.Minute, "my_id2")
					Ω(err).ShouldNot(HaveOccurred())
					defer func() {
						releasedLock := make(chan bool)
						releaseLock <- releasedLock
						<-releasedLock
					}()

					secondConvergeDidGrabLock := false
					go func() {
						bbs.MaintainConvergeLock(1*time.Minute, "my_id2")
						secondConvergeDidGrabLock = true
					}()

					Consistently(secondConvergeDidGrabLock, 3.0).Should(BeFalse())
				})

				Context("when the lock disappears after it has been acquired (e.g. ETCD store is reset)", func() {
					It("should send a notification down the lostLockChannel", func() {
						lostLock, _, err := bbs.MaintainConvergeLock(1*time.Second, "my_id")
						Ω(err).ShouldNot(HaveOccurred())

						etcdRunner.Stop()

						Eventually(lostLock).Should(Receive())
					})
				})
			})

			Context("when releasing the lock", func() {
				It("makes it available for others trying to acquire it", func() {
					_, releaseLock, err := bbs.MaintainConvergeLock(1*time.Minute, "my_id")
					Ω(err).ShouldNot(HaveOccurred())

					gotLock := make(chan bool)
					go func() {
						_, newRelease, err := bbs.MaintainConvergeLock(1*time.Minute, "my_id")
						Ω(err).ShouldNot(HaveOccurred())

						releaseLock = newRelease
						close(gotLock)
					}()

					Consistently(gotLock, 1.0).ShouldNot(Receive())

					releasedLock := make(chan bool)
					releaseLock <- releasedLock

					Eventually(releasedLock).Should(BeClosed())
					Eventually(gotLock, 2.0).Should(BeClosed())
				})
			})
		})
	})
})
