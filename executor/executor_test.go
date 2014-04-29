package executor_test

import (
	"fmt"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/task_handler/fake_task_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Executor", func() {
	var (
		bbs            *Bbs.BBS
		task           *models.Task
		executor       *Executor
		taskRegistry   *task_registry.TaskRegistry
		wardenClient   *fake_warden_client.FakeClient
		ready          chan bool
		startingMemory int
		startingDisk   int
		storeAdapter   storeadapter.StoreAdapter
	)

	var fakeTaskHandler *fake_task_handler.FakeTaskHandler

	BeforeEach(func() {
		fakeTaskHandler = fake_task_handler.New()
		ready = make(chan bool, 1)

		storeAdapter = etcdRunner.Adapter()
		bbs = Bbs.New(storeAdapter, timeprovider.NewTimeProvider())
		wardenClient = fake_warden_client.New()

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
})
