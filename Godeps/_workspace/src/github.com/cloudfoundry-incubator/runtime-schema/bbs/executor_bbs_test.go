package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"path"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var _ = Describe("Executor BBS", func() {
	var bbs *BBS
	var runOnce models.RunOnce
	var timeToClaim time.Duration

	BeforeEach(func() {
		timeToClaim = 1 * time.Second
		bbs = New(store)
		runOnce = models.RunOnce{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
			CreatedAt:       time.Now().UnixNano(),
		}
	})

	Describe("ConvergeRunOnce", func() {
		var otherRunOnce models.RunOnce

		BeforeEach(func() {
			otherRunOnce = models.RunOnce{
				Guid:      "some-other-guid",
				CreatedAt: time.Now().UnixNano(),
			}
		})

		Context("when a pending key exists", func() {
			JustBeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("and it is unclaimed", func() {
				Context("and it is more than the handling timeToClaim after the createdAt", func() {
					BeforeEach(func() {
						runOnce.CreatedAt = runOnce.CreatedAt - int64(timeToClaim)
					})

					It("should mark it as failed", func() {
						bbs.ConvergeRunOnce(timeToClaim)
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(1))
						Ω(completedRunOnces[0].Failed).Should(BeTrue())
						Ω(completedRunOnces[0].FailureReason).Should(ContainSubstring("time limit"))
					})
				})

				Context("and it is less than the timeToClaim", func() {
					It("should kick the key", func(done Done) {
						defer close(done)
						events, _, _ := bbs.WatchForDesiredRunOnce()

						bbs.ConvergeRunOnce(timeToClaim)

						Ω(<-events).Should(Equal(runOnce))
					})
				})
			})

			Context("and the content of the node is corrupt", func() {
				JustBeforeEach(func() {
					store.Update(storeadapter.StoreNode{
						Key:   path.Join(RunOnceSchemaRoot, "pending", runOnce.Guid),
						Value: []byte("'"),
					})
				})

				It("should mark it as failed", func() {
					bbs.ConvergeRunOnce(timeToClaim)
					completedRunOnces, err := bbs.GetAllCompletedRunOnces()
					Ω(err).ShouldNot(HaveOccurred())
					Ω(completedRunOnces).Should(HaveLen(1))
					Ω(completedRunOnces[0].Failed).Should(BeTrue())
					Ω(completedRunOnces[0].FailureReason).Should(ContainSubstring("corrupt"))
				})
			})

			Context("and there is a claim key", func() {
				JustBeforeEach(func() {
					err := bbs.ClaimRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce(timeToClaim)

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should not delete the claim key", func() {
					bbs.ConvergeRunOnce(timeToClaim)

					_, err := store.Get("/v1/run_once/claimed/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})

				Context("and the associated executor is still alive", func() {
					var presence PresenceInterface

					JustBeforeEach(func() {
						var err error
						presence, _, err = bbs.MaintainExecutorPresence(10*time.Second, runOnce.ExecutorID)
						Ω(err).ShouldNot(HaveOccurred())
					})

					AfterEach(func() {
						presence.Remove()
					})

					It("should not mark the task as completed/failed", func() {
						bbs.ConvergeRunOnce(timeToClaim)
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(0))
					})
				})

				Context("and the associated executor has gone missing", func() {
					It("should mark the RunOnce as completed (in the failed state)", func() {
						bbs.ConvergeRunOnce(timeToClaim)
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(1))
						Ω(completedRunOnces[0].Failed).Should(BeTrue())
						Ω(completedRunOnces[0].FailureReason).Should(ContainSubstring("executor"))
					})
				})
			})

			Context("and there is a running key", func() {
				JustBeforeEach(func() {
					err := bbs.StartRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce(timeToClaim)

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should not delete the running key", func() {
					bbs.ConvergeRunOnce(timeToClaim)

					_, err := store.Get("/v1/run_once/running/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})

				Context("and the associated executor is still alive", func() {
					var presence PresenceInterface

					JustBeforeEach(func() {
						var err error
						presence, _, err = bbs.MaintainExecutorPresence(10*time.Second, runOnce.ExecutorID)
						Ω(err).ShouldNot(HaveOccurred())
					})

					AfterEach(func() {
						presence.Remove()
					})

					It("should not mark the task as completed/failed", func() {
						bbs.ConvergeRunOnce(timeToClaim)
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(0))
					})
				})

				Context("and the associated executor has gone missing", func() {
					It("should mark the RunOnce as completed (in the failed state)", func() {
						bbs.ConvergeRunOnce(timeToClaim)
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(1))
						Ω(completedRunOnces[0].Failed).Should(BeTrue())
						Ω(completedRunOnces[0].FailureReason).Should(ContainSubstring("executor"))
					})
				})
			})

			Context("and there is a completed key", func() {
				JustBeforeEach(func() {
					err := bbs.CompleteRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce(timeToClaim)

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should kick the completed key", func(done Done) {
					events, _, _ := bbs.WatchForCompletedRunOnce()

					bbs.ConvergeRunOnce(timeToClaim)

					Ω(<-events).Should(Equal(runOnce))

					close(done)
				})

				It("should not delete the completed key", func() {
					bbs.ConvergeRunOnce(timeToClaim)

					_, err := store.Get("/v1/run_once/completed/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("and there are no other keys", func() {
				It("should kick the pending key",
					func(done Done) {
						events, _, _ := bbs.WatchForDesiredRunOnce()

						bbs.ConvergeRunOnce(timeToClaim)

						Ω(<-events).Should(Equal(runOnce))

						close(done)
					})
			})
		})

		Context("when a pending key does not exist", func() {
			JustBeforeEach(func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete any extra keys", func() {
				bbs.ConvergeRunOnce(timeToClaim)

				_, err := store.Get("/v1/run_once/claimed/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/running/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/completed/some-guid")
				Ω(err).Should(HaveOccurred())
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
