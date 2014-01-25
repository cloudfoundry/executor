package executor_test

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/cloudfoundry-incubator/executor/executor"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("Executor", func() {
	var (
		bbs      *Bbs.BBS
		runOnce  models.RunOnce
		executor *Executor
		gordon   *fake_gordon.FakeGordon
	)

	BeforeEach(func() {
		bbs = Bbs.New(etcdRunner.Adapter())
		gordon = fake_gordon.New()

		executor = New(bbs, gordon)

		runOnce = models.RunOnce{
			Guid: "totally-unique",
		}
	})

	Describe("Executor IDs", func() {
		It("should generate a random ID when created", func() {
			executor1 := New(bbs, gordon)
			executor2 := New(bbs, gordon)

			Ω(executor1.ID()).ShouldNot(BeZero())
			Ω(executor2.ID()).ShouldNot(BeZero())

			Ω(executor1.ID()).ShouldNot(Equal(executor2.ID()))
		})
	})

	Describe("Handling RunOnces", func() {
		Context("when it sees a desired RunOnce", func() {
			BeforeEach(func() {
				executor.HandleRunOnces()
			})

			AfterEach(func() {
				executor.StopHandlingRunOnces()
			})

			Context("when all is well", func() {
				BeforeEach(func() {
					err := bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("eventually is claimed", func() {
					Eventually(func() []models.RunOnce {
						runOnces, err := bbs.GetAllClaimedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						return runOnces
					}).Should(HaveLen(1))

					runOnces, _ := bbs.GetAllClaimedRunOnces()
					runningRunOnce := runOnces[0]
					Ω(runningRunOnce.Guid).Should(Equal(runOnce.Guid))
					Ω(runningRunOnce.ExecutorID).Should(Equal(executor.ID()))
				})

				It("eventually creates a container and starts running", func() {
					Eventually(func() []models.RunOnce {
						runOnces, err := bbs.GetAllStartingRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						return runOnces
					}).Should(HaveLen(1))

					runOnces, _ := bbs.GetAllStartingRunOnces()
					runningRunOnce := runOnces[0]
					Ω(runningRunOnce.Guid).Should(Equal(runOnce.Guid))
					Ω(gordon.CreatedHandles()).Should(ContainElement(runningRunOnce.ContainerHandle))
				})
			})

			Context("but it's already been claimed", func() {
				BeforeEach(func() {
					runOnce.ExecutorID = "fitter, faster, more educated"
					err := bbs.ClaimRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())

					err = bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("bails", func() {
					time.Sleep(1 * time.Second)

					runOnces, _ := bbs.GetAllStartingRunOnces()
					Ω(runOnces).Should(BeEmpty())
					Ω(gordon.CreatedHandles()).Should(BeEmpty())
				})
			})

			Context("when it fails to make a container", func() {
				BeforeEach(func() {
					gordon.CreateError = errors.New("No container for you")

					err := bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("bails without creating a starting RunOnce", func() {
					Eventually(func() []models.RunOnce {
						runOnces, _ := bbs.GetAllClaimedRunOnces()
						return runOnces
					}).Should(HaveLen(1))

					time.Sleep(1 * time.Second)

					runOnces, _ := bbs.GetAllStartingRunOnces()
					Ω(runOnces).Should(BeEmpty())
					Ω(gordon.CreatedHandles()).Should(BeEmpty())
				})
			})

			Context("when it fails to create a start RunOnce", func() {
				BeforeEach(func() {
					runOnce.ExecutorID = "this really shouldn't happen..."
					runOnce.ContainerHandle = "...but somehow it did."
					err := bbs.StartRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())

					err = bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should destroy the container", func() {
					Eventually(func() []models.RunOnce {
						runOnces, _ := bbs.GetAllClaimedRunOnces()
						return runOnces
					}).Should(HaveLen(1))

					Eventually(func() []string { return gordon.DestroyedHandles() }).Should(HaveLen(1))
					Ω(gordon.DestroyedHandles()).Should(Equal(gordon.CreatedHandles()))
				})
			})
		})

		Context("when ETCD disappears then reappers", func() {
			BeforeEach(func() {
				executor.HandleRunOnces()

				etcdRunner.Stop()
				time.Sleep(200 * time.Millisecond) //give the etcd driver time to realize we timed out.  the etcd driver is hardcoded to have a 200 ms timeout

				etcdRunner.Start()
				time.Sleep(200 * time.Millisecond) //give the etcd driver a chance to connect

				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			AfterEach(func() {
				executor.StopHandlingRunOnces()
			})

			It("should handle any new desired RunOnces", func() {
				Eventually(func() []models.RunOnce {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}).Should(HaveLen(1))
			})
		})

		Context("when told to stop handling RunOnces", func() {
			BeforeEach(func() {
				executor.HandleRunOnces()
				executor.StopHandlingRunOnces()
			})

			It("does not handle any new desired RunOnces", func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				time.Sleep(1 * time.Second)
				runOnces, _ := bbs.GetAllClaimedRunOnces()
				Ω(runOnces).Should(BeEmpty())
			})
		})

		Context("when two executors are fighting for a RunOnce", func() {
			var otherExecutor *Executor

			BeforeEach(func() {
				executor.HandleRunOnces()

				otherExecutor = New(bbs, gordon)
				otherExecutor.HandleRunOnces()
			})

			AfterEach(func() {
				executor.StopHandlingRunOnces()
				otherExecutor.StopHandlingRunOnces()
			})

			It("the winner should be randomly distributed", func() {
				samples := 40

				//generate N desired run onces
				for i := 0; i < samples; i++ {
					runOnce := models.RunOnce{
						Guid: fmt.Sprintf("totally-unique-%d", i),
					}
					err := bbs.DesireRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				}

				//eventually all N should be claimed
				Eventually(func() []models.RunOnce {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}, 5).Should(HaveLen(samples))

				//figure out who claimed the run onces
				claimedRunOnces, _ := bbs.GetAllClaimedRunOnces()
				handlers := map[string]int{}

				for _, claimedRunOnce := range claimedRunOnces {
					handlers[claimedRunOnce.ExecutorID] += 1
				}

				//assert that at least both executors are participating
				//these might appear flakey, but the odds of failing should be really really low...
				Ω(handlers).Should(HaveLen(2))
				Ω(handlers[executor.ID()]).Should(BeNumerically(">", 3))
				Ω(handlers[otherExecutor.ID()]).Should(BeNumerically(">", 3))
			})
		})
	})
})
