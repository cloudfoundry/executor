package executor_test

import (
	"fmt"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter"
	"github.com/onsi/ginkgo/config"
	"time"

	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/fake_run_once_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("Executor", func() {
	var (
		bbs              *Bbs.BBS
		runOnce          models.RunOnce
		executor         *Executor
		taskRegistry     *task_registry.TaskRegistry
		gordon           *fake_gordon.FakeGordon
		registryFileName string
		ready            chan bool
		startingMemory   int
		startingDisk     int
	)

	var fakeRunOnceHandler *fake_run_once_handler.FakeRunOnceHandler

	BeforeEach(func() {
		fakeRunOnceHandler = fake_run_once_handler.New()
		ready = make(chan bool, 1)

		registryFileName = fmt.Sprintf("/tmp/executor_registry_%d", config.GinkgoConfig.ParallelNode)

		bbs = Bbs.New(etcdRunner.Adapter())
		gordon = fake_gordon.New()

		startingMemory = 256
		startingDisk = 1024
		taskRegistry = task_registry.NewTaskRegistry("some-stack", registryFileName, startingMemory, startingDisk)

		runOnce = models.RunOnce{
			Guid:     "totally-unique",
			MemoryMB: 256,
			DiskMB:   1024,
			Stack:    "some-stack",
		}

		executor = New(bbs, steno.NewLogger("test-logger"))
	})

	Describe("Executor IDs", func() {
		It("should generate a random ID when created", func() {
			executor1 := New(bbs, steno.NewLogger("test-logger"))
			executor2 := New(bbs, steno.NewLogger("test-logger"))

			Ω(executor1.ID()).ShouldNot(BeZero())
			Ω(executor2.ID()).ShouldNot(BeZero())

			Ω(executor1.ID()).ShouldNot(Equal(executor2.ID()))
		})
	})

	Describe("Handling", func() {
		BeforeEach(func() {
			go executor.Handle(fakeRunOnceHandler, ready)
			<-ready
		})

		Context("when ETCD disappears then reappers", func() {
			BeforeEach(func() {
				etcdRunner.Stop()
				time.Sleep(200 * time.Millisecond) //give the etcd driver time to realize we timed out.  the etcd driver is hardcoded to have a 200 ms timeout

				etcdRunner.Start()
				time.Sleep(200 * time.Millisecond) //give the etcd driver a chance to connect

				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			AfterEach(func() {
				executor.Stop()
			})

			It("should handle any new desired RunOnces", func() {
				Eventually(func() int {
					return fakeRunOnceHandler.NumberOfCalls()
				}).Should(Equal(1))
			})
		})

		Context("when told to stop", func() {
			BeforeEach(func() {
				executor.Stop()
			})

			It("does not handle any new desired RunOnces", func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				Consistently(func() int {
					return fakeRunOnceHandler.NumberOfCalls()
				}).Should(Equal(0))
			})
		})

		Context("when two executors are fighting for a RunOnce", func() {
			var otherExecutor *Executor

			BeforeEach(func() {
				otherExecutor = New(bbs, steno.NewLogger("test-logger"))

				go otherExecutor.Handle(fakeRunOnceHandler, ready)
				<-ready
			})

			AfterEach(func() {
				executor.Stop()
				otherExecutor.Stop()
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

				//eventually the runoncehandlers should have been called N times
				Eventually(func() int {
					return fakeRunOnceHandler.NumberOfCalls()
				}, 5).Should(Equal(samples))

				var numberHandledByFirst int
				var numberHandledByOther int
				for _, executorId := range fakeRunOnceHandler.HandledRunOnces() {
					if executor.ID() == executorId {
						numberHandledByFirst++
					} else if otherExecutor.ID() == executorId {
						numberHandledByOther++
					}
				}
				Ω(numberHandledByFirst).Should(BeNumerically(">", 3))
				Ω(numberHandledByOther).Should(BeNumerically(">", 3))
			})
		})
	})

	Describe("Maintaining Presence", func() {
		It("should maintain presence", func() {
			err := executor.MaintainPresence(60 * time.Second)
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
				err := executor.MaintainPresence(60 * time.Second)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when we fail to maintain our presence", func() {
			var handleErr chan error

			BeforeEach(func() {
				handleErr = make(chan error, 1)

				go func() {
					handleErr <- executor.Handle(fakeRunOnceHandler, ready)
				}()

				<-ready

				executor.MaintainPresence(1 * time.Second)
			})

			triggerMaintainPresenceFailure := func() {
				time.Sleep(1 * time.Second)
				// delete its key (and everything else lol)
				etcdRunner.Reset()
				time.Sleep(2 * time.Second)
			}

			It("cancels the running actions and returns an error", func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(fakeRunOnceHandler.HandledRunOnces).ShouldNot(BeEmpty())

				triggerMaintainPresenceFailure()

				Eventually(fakeRunOnceHandler.GetCancel).ShouldNot(BeNil())
				Eventually(fakeRunOnceHandler.GetCancel()).Should(BeClosed())

				Eventually(handleErr).Should(Receive(&err))

				Ω(err).Should(Equal(MaintainPresenceError))
			})
		})

		Context("when told to stop", func() {
			It("it removes its presence", func() {
				err := executor.MaintainPresence(60 * time.Second)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(func() interface{} {
					arr, _ := bbs.GetAllExecutors()
					return arr
				}).Should(HaveLen(1))

				executor.Stop()

				executors, err := bbs.GetAllExecutors()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(executors).Should(HaveLen(0))
			})
		})
	})

	Describe("Converging RunOnces", func() {
		var fakeExecutorBBS *fake_bbs.FakeExecutorBBS
		BeforeEach(func() {
			fakeExecutorBBS = &fake_bbs.FakeExecutorBBS{}
			bbs.ExecutorBBS = fakeExecutorBBS
		})

		It("converges runOnces on a regular interval", func() {
			stopChannel := executor.ConvergeRunOnces(10*time.Millisecond, 30*time.Second)
			defer func() {
				stopChannel <- true
			}()

			Eventually(func() int {
				return fakeExecutorBBS.CallsToConverge
			}, 1.0, 0.1).Should(BeNumerically(">", 2))

			Ω(fakeExecutorBBS.ConvergeRunOnceTimeToClaim).Should(Equal(30 * time.Second))
		})

		It("converges immediately without waiting for the iteration", func() {
			stopChannel := executor.ConvergeRunOnces(1*time.Minute, 30*time.Second)
			defer func() {
				stopChannel <- true
			}()

			Eventually(func() int {
				return fakeExecutorBBS.CallsToConverge
			}, 1.0, 0.1).Should(BeNumerically("==", 1))
			Ω(fakeExecutorBBS.ConvergeRunOnceTimeToClaim).Should(Equal(30 * time.Second))
		})

		It("stops convergence when told", func() {
			stopChannel := executor.ConvergeRunOnces(10*time.Millisecond, 30*time.Second)

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
			fakeExecutorBBS.MaintainConvergeLockError = storeadapter.ErrorKeyExists

			stopChannel := executor.ConvergeRunOnces(10*time.Millisecond, 30*time.Second)
			defer func() {
				stopChannel <- true
			}()

			time.Sleep(20 * time.Millisecond)
			Ω(fakeExecutorBBS.CallsToConverge).To(Equal(0))
		})

		It("logs a debug message when GrabLock fails", func() {
			fakeExecutorBBS.MaintainConvergeLockError = storeadapter.ErrorKeyExists

			stopChannel := executor.ConvergeRunOnces(10*time.Millisecond, 30*time.Second)
			defer func() {
				stopChannel <- true
			}()

			testSink := steno.GetMeTheGlobalTestSink()

			lockMessageIndex := 0
			Eventually(func() string {
				if len(testSink.Records) > 0 {
					lockMessageIndex := len(testSink.Records) - 1
					return testSink.Records[lockMessageIndex].Message
				}
				return ""
			}, 1.0, 0.1).Should(Equal("error when maintaining converge lock"))

			Ω(testSink.Records[lockMessageIndex].Level).Should(Equal(steno.LOG_DEBUG))
		})
	})
})
