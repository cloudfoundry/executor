package executor_test

import (
	"errors"
	"fmt"
	"github.com/onsi/ginkgo/config"
	"time"

	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("Executor", func() {
	var (
		bbs              *Bbs.BBS
		runOnce          models.RunOnce
		executor         *Executor
		taskRegistry     *taskregistry.TaskRegistry
		gordon           *fake_gordon.FakeGordon
		testSink         *steno.TestingSink
		registryFileName string
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
		taskRegistry = taskregistry.NewTaskRegistry(registryFileName, 256, 1024)

		runOnce = models.RunOnce{
			Guid:     "totally-unique",
			MemoryMB: 256,
			DiskMB:   1024,
		}

		executor = New(bbs, gordon, taskRegistry)
	})

	Describe("Executor IDs", func() {
		It("should generate a random ID when created", func() {
			executor1 := New(bbs, gordon, taskRegistry)
			executor2 := New(bbs, gordon, taskRegistry)

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

				It("should clean up after the RunOnce is finished", func() {
					//Since the first runOnce fills the system to capacity, we can test cleanup by
					//running a second runOnce of the same size.

					//Wait until the first runOnce finishes
					Eventually(func() []models.RunOnce {
						runOnces, err := bbs.GetAllCompletedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						return runOnces
					}, 1.5).Should(HaveLen(1))

					bbs.ResolveRunOnce(runOnce)

					Eventually(func() []models.RunOnce {
						runOnces, err := bbs.GetAllPendingRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						return runOnces
					}).Should(HaveLen(0))

					runTwice := models.RunOnce{
						Guid:     "not-totally-unique",
						MemoryMB: 256,
						DiskMB:   1024,
					}

					err := bbs.DesireRunOnce(runTwice)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(func() []models.RunOnce {
						runOnces, err := bbs.GetAllClaimedRunOnces()
						Ω(err).ShouldNot(HaveOccurred())
						return runOnces
					}, 1.5).Should(HaveLen(2))

					runOnces, _ := bbs.GetAllClaimedRunOnces()
					runningRunOnce := runOnces[1]
					Ω(runningRunOnce.Guid).Should(Equal(runTwice.Guid))
					Ω(runningRunOnce.ExecutorID).Should(Equal(executor.ID()))
				}, 5.0)
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

				Consistently(func() []models.RunOnce {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}).Should(BeEmpty())
			})
		})

		Context("when the executor is out of resources", func() {
			BeforeEach(func() {
				executor.HandleRunOnces()
			})

			AfterEach(func() {
				executor.StopHandlingRunOnces()
			})

			It("doesn't pick up another desired RunOnce", func() {
				err := bbs.DesireRunOnce(models.RunOnce{
					Guid:     "Let me use all of your memory!",
					MemoryMB: 256,
					Actions: []models.ExecutorAction{
						{models.CopyAction{From: "thing", To: "other thing", Extract: false, Compress: true}},
					},
				})
				Eventually(func() []models.RunOnce {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}).Should(HaveLen(1))

				err = bbs.DesireRunOnce(models.RunOnce{
					Guid:     "I want some memory!",
					MemoryMB: 1,
				})
				Ω(err).ShouldNot(HaveOccurred())

				Consistently(func() []models.RunOnce {
					runOnces, _ := bbs.GetAllClaimedRunOnces()
					return runOnces
				}, 0.33).Should(HaveLen(1))
			}, 5.0)
		})

		Context("when two executors are fighting for a RunOnce", func() {
			var otherExecutor *Executor

			BeforeEach(func() {
				executor.HandleRunOnces()

				otherExecutor = New(bbs, gordon, taskRegistry)
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

	Describe("Maintaining Presence", func() {
		It("should maintain presence", func() {
			err := executor.MaintainPresence(60)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(func() interface{} {
				arr, _ := bbs.GetAllExecutors()
				return arr
			}).Should(HaveLen(1))

			executors, err := bbs.GetAllExecutors()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(executors[0]).Should(Equal(executor.ID()))
		})

		Context("when maintaining presence fails to start", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("should return an error", func() {
				err := executor.MaintainPresence(60)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when we fail to maintain our presence", func() {
			BeforeEach(func() {
				executor.HandleRunOnces()

				executor.MaintainPresence(1)
			})

			It("stops handling RunOnces", func() {
				time.Sleep(1 * time.Second)

				// delete its key (and everything else lol)
				etcdRunner.Reset()

				time.Sleep(2 * time.Second)

				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				Consistently(func() []models.RunOnce {
					runOnces, err := bbs.GetAllClaimedRunOnces()
					Ω(err).ShouldNot(HaveOccurred())
					return runOnces
				}).Should(HaveLen(0))
			})
		})
	})

	Describe("Convergence", func() {
		var fakeExecutorBBS *fakebbs.FakeExecutorBBS
		BeforeEach(func() {
			fakeExecutorBBS = &fakebbs.FakeExecutorBBS{LockIsGrabbable: true}
			bbs.ExecutorBBS = fakeExecutorBBS
		})

		It("converges runOnces on a regular interval", func() {
			stopChannel := executor.ConvergeRunOnces(10 * time.Millisecond)
			defer func() {
				stopChannel <- true
			}()

			Eventually(func() int {
				return fakeExecutorBBS.CallsToConverge
			}, 1.0, 0.1).Should(BeNumerically(">", 2))
		})

		It("converges immediately without waiting for the iteration", func() {
			stopChannel := executor.ConvergeRunOnces(1 * time.Minute)
			defer func() {
				stopChannel <- true
			}()

			Eventually(func() int {
				return fakeExecutorBBS.CallsToConverge
			}, 1.0, 0.1).Should(BeNumerically("==", 1))
		})

		It("stops convergence when told", func() {
			stopChannel := executor.ConvergeRunOnces(10 * time.Millisecond)

			count := 1
			Eventually(func() int {
				if fakeExecutorBBS.CallsToConverge > 0 && stopChannel != nil {
					stopChannel <- true
					stopChannel = nil
				}
				diff := fakeExecutorBBS.CallsToConverge - count
				count = fakeExecutorBBS.CallsToConverge
				return diff
			}, 1.0, 0.1).Should(Equal(0))
		})

		It("should only converge if it has the lock", func() {
			fakeExecutorBBS.LockIsGrabbable = false

			stopChannel := executor.ConvergeRunOnces(10 * time.Millisecond)
			defer func() {
				stopChannel <- true
			}()

			time.Sleep(20 * time.Millisecond)
			Ω(fakeExecutorBBS.CallsToConverge).To(Equal(0))
		})

		It("logs an error message when GrabLock fails", func() {
			fakeExecutorBBS.ErrorOnGrabLock = errors.New("Danger! Will Robinson, Danger!")
			stopChannel := executor.ConvergeRunOnces(10 * time.Millisecond)
			defer func() {
				stopChannel <- true
			}()

			Eventually(func() string {
				if len(testSink.Records) > 0 {
					return testSink.Records[len(testSink.Records)-1].Message
				}
				return ""
			}, 1.0, 0.1).Should(Equal("error when grabbing converge lock"))
		})
	})
})
