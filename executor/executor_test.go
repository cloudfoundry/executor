package executor_test

import (
	"fmt"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/fake_run_once_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Executor", func() {
	var (
		bbs            *Bbs.BBS
		task        *models.Task
		executor       *Executor
		taskRegistry   *task_registry.TaskRegistry
		gordon         *fake_gordon.FakeGordon
		ready          chan bool
		startingMemory int
		startingDisk   int
		storeAdapter   storeadapter.StoreAdapter
	)

	var fakeTaskHandler *fake_run_once_handler.FakeTaskHandler

	BeforeEach(func() {
		fakeTaskHandler = fake_run_once_handler.New()
		ready = make(chan bool, 1)

		storeAdapter = etcdRunner.Adapter()
		bbs = Bbs.New(storeAdapter, timeprovider.NewTimeProvider())
		gordon = fake_gordon.New()

		startingMemory = 256
		startingDisk = 1024
		taskRegistry = task_registry.NewTaskRegistry(
			"some-stack",
			startingMemory,
			startingDisk,
		)

		task = &models.Task{
			Guid:     "totally-unique",
			MemoryMB: 256,
			DiskMB:   1024,
			Stack:    "some-stack",
		}

		executor = New(bbs, 0, steno.NewLogger("test-logger"))
	})

	AfterEach(func() {
		executor.Stop()
		storeAdapter.Disconnect()
	})

	Describe("Executor IDs", func() {
		It("should generate a random ID when created", func() {
			executor1 := New(bbs, 0, steno.NewLogger("test-logger"))
			executor2 := New(bbs, 0, steno.NewLogger("test-logger"))

			Ω(executor1.ID()).ShouldNot(BeZero())
			Ω(executor2.ID()).ShouldNot(BeZero())

			Ω(executor1.ID()).ShouldNot(Equal(executor2.ID()))
		})
	})

	Describe("Handling", func() {
		BeforeEach(func() {
			go executor.Handle(fakeTaskHandler, ready)
			<-ready
		})

		Context("when ETCD disappears then reappers", func() {
			BeforeEach(func() {
				etcdRunner.Stop()

				// give the etcd driver time to realize we timed out.  the etcd driver
				// is hardcoded to have a 200 ms timeout
				time.Sleep(200 * time.Millisecond)

				etcdRunner.Start()

				// give the etcd driver a chance to connect
				time.Sleep(200 * time.Millisecond)

				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should handle any new desired Tasks", func() {
				Eventually(func() int {
					return fakeTaskHandler.NumberOfCalls()
				}).Should(Equal(1))
			})
		})

		Context("when told to stop", func() {
			BeforeEach(func() {
				executor.Stop()
			})

			It("does not handle any new desired Tasks", func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				Consistently(func() int {
					return fakeTaskHandler.NumberOfCalls()
				}).Should(Equal(0))
			})
		})

		Context("when two executors are fighting for a Task", func() {
			var otherExecutor *Executor

			BeforeEach(func() {
				otherExecutor = New(bbs, 0, steno.NewLogger("test-logger"))

				go otherExecutor.Handle(fakeTaskHandler, ready)
				<-ready
			})

			AfterEach(func() {
				otherExecutor.Stop()
			})

			It("the winner should be randomly distributed", func() {
				samples := 40

				//generate N desired run onces
				for i := 0; i < samples; i++ {
					task := &models.Task{
						Guid: fmt.Sprintf("totally-unique-%d", i),
					}
					err := bbs.DesireTask(task)
					Ω(err).ShouldNot(HaveOccurred())
				}

				//eventually the taskhandlers should have been called N times
				Eventually(func() int {
					return fakeTaskHandler.NumberOfCalls()
				}, 5).Should(Equal(samples))

				var numberHandledByFirst int
				var numberHandledByOther int
				for _, executorId := range fakeTaskHandler.HandledTasks() {
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
		var gainedPresence chan error
		var lostPresence chan error

		var heartbeatInterval time.Duration

		BeforeEach(func() {
			heartbeatInterval = 60 * time.Second
		})

		JustBeforeEach(func() {
			gainedPresence = make(chan error, 1)
			lostPresence = make(chan error, 1)

			go executor.MaintainPresence(heartbeatInterval, gainedPresence, lostPresence)

			var err error
			Eventually(gainedPresence).Should(Receive(&err))
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should maintain presence", func() {
			Eventually(bbs.GetAllExecutors).Should(HaveLen(1))

			executors, err := bbs.GetAllExecutors()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(executors[0]).Should(Equal(executor.ID()))
		})

		Context("when we fail to maintain our presence", func() {
			BeforeEach(func() {
				heartbeatInterval = 1 * time.Second
			})

			triggerMaintainPresenceFailure := func() {
				time.Sleep(1 * time.Second)

				// delete the executor's key (and everything else lol)
				etcdRunner.Reset()

				time.Sleep(2 * time.Second)
			}

			It("sends an error to the given error channel", func() {
				triggerMaintainPresenceFailure()

				var err error
				Eventually(lostPresence).Should(Receive(&err))
				Ω(err).Should(Equal(ErrLostPresence))
			})
		})

		Context("when told to stop", func() {
			It("it removes its presence", func() {
				Eventually(bbs.GetAllExecutors).Should(HaveLen(1))

				executor.Stop()

				executors, err := bbs.GetAllExecutors()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(executors).Should(HaveLen(0))
			})
		})
	})

	Describe("Converging Tasks", func() {
		var fakeExecutorBBS *fake_bbs.FakeExecutorBBS
		BeforeEach(func() {
			fakeExecutorBBS = &fake_bbs.FakeExecutorBBS{}
			bbs.ExecutorBBS = fakeExecutorBBS
		})

		It("converges tasks on a regular interval", func() {
			go executor.ConvergeTasks(10*time.Millisecond, 30*time.Second)

			Eventually(fakeExecutorBBS.CallsToConverge, 1.0, 0.1).Should(BeNumerically(">", 2))

			Ω(fakeExecutorBBS.ConvergeTimeToClaimTasks()).Should(Equal(30 * time.Second))
		})

		It("converges immediately without waiting for the iteration", func() {
			go executor.ConvergeTasks(24*time.Hour, 30*time.Second)

			Eventually(fakeExecutorBBS.CallsToConverge, 1.0, 0.1).Should(BeNumerically("==", 1))
			Ω(fakeExecutorBBS.ConvergeTimeToClaimTasks()).Should(Equal(30 * time.Second))
		})

		It("stops convergence when told", func() {
			go executor.ConvergeTasks(10*time.Millisecond, 30*time.Second)

			count := 1
			Eventually(func() int {
				calls := fakeExecutorBBS.CallsToConverge()

				if calls > 0 {
					executor.Stop()
				}

				diff := calls - count
				count = calls

				return diff
			}, 1.0, 0.1).Should(Equal(0))
		})

		Context("when the converge lock cannot be acquired", func() {
			BeforeEach(func() {
				fakeExecutorBBS.SetMaintainConvergeLockError(storeadapter.ErrorKeyExists)
			})

			It("should only converge if it has the lock", func() {
				fakeExecutorBBS.SetMaintainConvergeLockError(storeadapter.ErrorKeyExists)

				go executor.ConvergeTasks(10*time.Millisecond, 30*time.Second)

				Consistently(fakeExecutorBBS.CallsToConverge).Should(Equal(0))
			})

			It("logs an error message when GrabLock fails", func() {
				fakeExecutorBBS.SetMaintainConvergeLockError(storeadapter.ErrorKeyExists)

				go executor.ConvergeTasks(10*time.Millisecond, 30*time.Second)

				testSink := steno.GetMeTheGlobalTestSink()

				records := []*steno.Record{}

				lockMessageIndex := 0
				Eventually(func() string {
					records = testSink.Records()

					if len(records) > 0 {
						lockMessageIndex := len(records) - 1
						return records[lockMessageIndex].Message
					}

					return ""
				}, 1.0, 0.1).Should(Equal("error when creating converge lock"))

				Ω(records[lockMessageIndex].Level).Should(Equal(steno.LOG_ERROR))
			})
		})
	})
})
