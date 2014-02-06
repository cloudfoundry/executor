package runoncehandler_test

import (
	"errors"
	// "errors"
	"fmt"
	"github.com/cloudfoundry-incubator/executor/actionrunner/fakeactionrunner"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/taskregistry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("RunOnceHandler", func() {
	var (
		handler *RunOnceHandler

		bbs              *Bbs.BBS
		runOnce          models.RunOnce
		taskRegistry     *taskregistry.TaskRegistry
		gordon           *fake_gordon.FakeGordon
		actionRunner     *fakeactionrunner.FakeActionRunner
		testSink         *steno.TestingSink
		registryFileName string

		startingMemory int
		startingDisk   int
	)

	BeforeEach(func() {
		registryFileName = fmt.Sprintf("/tmp/executor_registry_%d", config.GinkgoConfig.ParallelNode)
		testSink = steno.NewTestingSink()
		stenoConfig := steno.Config{
			Sinks: []steno.Sink{testSink},
		}
		steno.Init(&stenoConfig)

		bbs = Bbs.New(etcdRunner.Adapter())
		gordon = fake_gordon.New()
		actionRunner = fakeactionrunner.New()

		startingMemory = 256
		startingDisk = 1024
		taskRegistry = taskregistry.NewTaskRegistry(registryFileName, startingMemory, startingDisk)

		runOnce = models.RunOnce{
			Guid:     "totally-unique",
			MemoryMB: 256,
			DiskMB:   1024,
			Actions: []models.ExecutorAction{
				{models.RunAction{"sudo reboot"}},
			},
		}

		handler = New(bbs, gordon, taskRegistry, actionRunner)
	})

	Describe("Handling a RunOnce", func() {
		JustBeforeEach(func() {
			go handler.RunOnce(runOnce, "executor-id")
		})

		Context("When there are enough resources to claim the RunOnce", func() {
			It("should allocate resources", func() {
				Eventually(func() interface{} {
					return taskRegistry.AvailableMemoryMB()
				}).Should(Equal(startingMemory - runOnce.MemoryMB))
				Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk - runOnce.DiskMB))
			})

			Context("When the RunOnce can be claimed", func() {
				var claimedRunOnce models.RunOnce
				JustBeforeEach(func() {
					Eventually(func() []models.RunOnce {
						runOnces, _ := bbs.GetAllClaimedRunOnces()
						return runOnces
					}).Should(HaveLen(1))

					runOnces, _ := bbs.GetAllClaimedRunOnces()
					claimedRunOnce = runOnces[0]
				})

				It("claims it", func() {
					Ω(claimedRunOnce.Guid).Should(Equal(runOnce.Guid))
					Ω(claimedRunOnce.ExecutorID).Should(Equal("executor-id"))
				})

				Context("When a container can be made", func() {
					JustBeforeEach(func() {
						Eventually(func() interface{} {
							return gordon.CreatedHandles()
						}).Should(HaveLen(1))
					})

					It("should make the container", func() {
						Ω(gordon.CreatedHandles()).Should(HaveLen(1))
					})

					Context("When the RunOnce can be put into the starting state", func() {
						var startingRunOnce models.RunOnce

						JustBeforeEach(func() {
							Eventually(func() []models.RunOnce {
								runOnces, err := bbs.GetAllStartingRunOnces()
								Ω(err).ShouldNot(HaveOccurred())
								return runOnces
							}).Should(HaveLen(1))

							runOnces, _ := bbs.GetAllStartingRunOnces()
							startingRunOnce = runOnces[0]

							Eventually(func() interface{} {
								return actionRunner.GetContainerHandle()
							}).ShouldNot(BeEmpty())
						})

						It("should mark the RunOnce as started", func() {
							Ω(startingRunOnce.Guid).Should(Equal(runOnce.Guid))
						})

						It("should start running the actions", func() {
							Ω(actionRunner.GetContainerHandle()).Should(Equal(gordon.CreatedHandles()[0]))
							Ω(actionRunner.GetActions()).Should(Equal(runOnce.Actions))
						})

						Context("when the RunOnce actions succeed", func() {
							JustBeforeEach(func() {
								actionRunner.ResolveWithoutError()
							})

							It("should deallocate resources and mark the RunOnce as completed (succesfully)", func() {
								Eventually(func() []models.RunOnce {
									runOnces, _ := bbs.GetAllCompletedRunOnces()
									return runOnces
								}).Should(HaveLen(1))

								runOnces, _ := bbs.GetAllCompletedRunOnces()
								Ω(runOnces[0].Failed).Should(BeFalse())

								Ω(taskRegistry.AvailableMemoryMB()).Should(Equal(startingMemory))
								Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk))
								Ω(gordon.DestroyedHandles()).Should(HaveLen(1))
							})
						})

						Context("when the RunOnce actions fail", func() {
							JustBeforeEach(func() {
								actionRunner.ResolveWithError(errors.New("Asplosions!"))
							})

							It("should deallocate resources and mark the RunOnce as completed (unsuccesfully)", func() {
								Eventually(func() []models.RunOnce {
									runOnces, _ := bbs.GetAllCompletedRunOnces()
									return runOnces
								}).Should(HaveLen(1))

								runOnces, _ := bbs.GetAllCompletedRunOnces()
								Ω(runOnces[0].Failed).Should(BeTrue())
								Ω(runOnces[0].FailureReason).Should(Equal("Asplosions!"))

								Ω(taskRegistry.AvailableMemoryMB()).Should(Equal(startingMemory))
								Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk))
								Ω(gordon.DestroyedHandles()).Should(HaveLen(1))
							})
						})
					})

					Context("When the RunOnce fails to be put into the starting state", func() {
						BeforeEach(func() {
							runOnce.ExecutorID = "this really shouldn't happen..."
							runOnce.ContainerHandle = "...but somehow it did."
							err := bbs.StartRunOnce(runOnce)
							Ω(err).ShouldNot(HaveOccurred())
						})

						It("should destroy the container and deallocate resources", func() {
							Eventually(func() []string { return gordon.DestroyedHandles() }).Should(HaveLen(1))
							Ω(gordon.DestroyedHandles()).Should(Equal(gordon.CreatedHandles()))

							Ω(taskRegistry.AvailableMemoryMB()).Should(Equal(startingMemory))
							Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk))
						})
					})
				})

				Context("when a container cannot be made", func() {
					BeforeEach(func() {
						gordon.CreateError = errors.New("No container for you")
					})

					It("does not create a starting RunOnce and it deallocates resources", func() {
						Consistently(func() interface{} {
							runOnces, _ := bbs.GetAllStartingRunOnces()
							return runOnces
						}, 0.3).Should(BeEmpty())

						Ω(taskRegistry.AvailableMemoryMB()).Should(Equal(startingMemory))
						Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk))
					})
				})
			})

			Context("When the RunOnce cannot be claimed", func() {
				BeforeEach(func() {
					runOnce.ExecutorID = "fitter, faster, more educated"
					err := bbs.ClaimRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("does not start the run once, create a container, or set aside resources for it", func() {
					Consistently(func() interface{} {
						runOnces, _ := bbs.GetAllStartingRunOnces()
						return runOnces
					}, 0.3).Should(BeEmpty())

					Ω(gordon.CreatedHandles()).Should(BeEmpty())

					Ω(taskRegistry.AvailableMemoryMB()).Should(Equal(startingMemory))
					Ω(taskRegistry.AvailableDiskMB()).Should(Equal(startingDisk))
				})
			})
		})

		Context("When there are not enough resources to claim the RunOnce", func() {
			BeforeEach(func() {
				runOnce.MemoryMB = startingMemory + 1
			})

			It("should not claim the run once or reserve resources", func() {
				Consistently(func() interface{} {
					return taskRegistry.AvailableMemoryMB()
				}).Should(Equal(startingMemory))

				Consistently(func() interface{} {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}, 0.3).Should(BeEmpty())
			})
		})
	})
})
