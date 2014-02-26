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

	BeforeEach(func() {
		bbs = New(store)
		runOnce = models.RunOnce{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
			CreatedAt:       time.Now(),
		}
	})

	Describe("ConvergeRunOnce", func() {
		var otherRunOnce models.RunOnce

		BeforeEach(func() {
			otherRunOnce = models.RunOnce{
				Guid:      "some-other-guid",
				CreatedAt: time.Now(),
			}
		})

		Context("when a pending key exists", func() {
			JustBeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("and it is unclaimed", func() {
				Context("and it is more than the handling timeout after the createdAt", func() {
					BeforeEach(func() {
						timeout := (1 * time.Second)

						runOnce.CreatedAt = runOnce.CreatedAt.Add(-timeout)
						bbs.SetTimeToClaim(timeout)
					})

					It("should mark it as failed", func() {
						bbs.ConvergeRunOnce()
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(1))
						Ω(completedRunOnces[0].Failed).Should(BeTrue())
						Ω(completedRunOnces[0].FailureReason).Should(ContainSubstring("time limit"))
					})
				})

				Context("and it is less than the timeout", func() {
					It("should kick the key", func(done Done) {
						events, _, _ := bbs.WatchForDesiredRunOnce()

						bbs.ConvergeRunOnce()

						Ω(<-events).Should(Equal(runOnce))

						close(done)
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
					bbs.ConvergeRunOnce()
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

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should not delete the claim key", func() {
					bbs.ConvergeRunOnce()

					_, err := store.Get("/v1/run_once/claimed/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})

				Context("and the associated executor is still alive", func() {
					var presence PresenceInterface

					JustBeforeEach(func() {
						var err error
						presence, _, err = bbs.MaintainExecutorPresence(10, runOnce.ExecutorID)
						Ω(err).ShouldNot(HaveOccurred())
					})

					AfterEach(func() {
						presence.Remove()
					})

					It("should not mark the task as completed/failed", func() {
						bbs.ConvergeRunOnce()
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(0))
					})
				})

				Context("and the associated executor has gone missing", func() {
					It("should mark the RunOnce as completed (in the failed state)", func() {
						bbs.ConvergeRunOnce()
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

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should not delete the running key", func() {
					bbs.ConvergeRunOnce()

					_, err := store.Get("/v1/run_once/running/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})

				Context("and the associated executor is still alive", func() {
					var presence PresenceInterface

					JustBeforeEach(func() {
						var err error
						presence, _, err = bbs.MaintainExecutorPresence(10, runOnce.ExecutorID)
						Ω(err).ShouldNot(HaveOccurred())
					})

					AfterEach(func() {
						presence.Remove()
					})

					It("should not mark the task as completed/failed", func() {
						bbs.ConvergeRunOnce()
						completedRunOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						Ω(completedRunOnces).Should(HaveLen(0))
					})
				})

				Context("and the associated executor has gone missing", func() {
					It("should mark the RunOnce as completed (in the failed state)", func() {
						bbs.ConvergeRunOnce()
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

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should kick the completed key", func(done Done) {
					events, _, _ := bbs.WatchForCompletedRunOnce()

					bbs.ConvergeRunOnce()

					Ω(<-events).Should(Equal(runOnce))

					close(done)
				})

				It("should not delete the completed key", func() {
					bbs.ConvergeRunOnce()

					_, err := store.Get("/v1/run_once/completed/some-guid")
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("and there are no other keys", func() {
				It("should kick the pending key",
					func(done Done) {
						events, _, _ := bbs.WatchForDesiredRunOnce()

						bbs.ConvergeRunOnce()

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
				bbs.ConvergeRunOnce()

				_, err := store.Get("/v1/run_once/claimed/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/running/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/completed/some-guid")
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
