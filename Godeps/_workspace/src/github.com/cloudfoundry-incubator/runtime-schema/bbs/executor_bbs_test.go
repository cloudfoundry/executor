package bbs_test

import (
	"github.com/cloudfoundry/storeadapter/test_helpers"
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
	var task *models.Task
	var timeToClaim time.Duration
	var presence Presence
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeToClaim = 30 * time.Second
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
		task = &models.Task{
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

	Describe("ClaimTask", func() {
		Context("when claiming a pending Task", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("puts the Task in the claim state", func() {
				err := bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.State).Should(Equal(models.TaskStateClaimed))
				Ω(task.ExecutorID).Should(Equal("executor-ID"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				err := bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ClaimTask(task, "executor-ID")
				})
			})
		})

		Context("when claiming a Task that is not in the pending state", func() {
			It("returns an error", func() {
				err := bbs.ClaimTask(task, "executor-ID")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("StartTask", func() {
		Context("when starting a claimed Task", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the state to running", func() {
				err := bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.State).Should(Equal(models.TaskStateRunning))
				Ω(task.ContainerHandle).Should(Equal("container-handle"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err := bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.StartTask(task, "container-handle")
				})
			})
		})

		Context("When starting a Task that is not in the claimed state", func() {
			It("returns an error", func() {
				err := bbs.StartTask(task, "container-handle")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("CompleteTask", func() {
		Context("when completing a running Task", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the Task in the completed state", func() {
				err := bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.Failed).Should(BeTrue())
				Ω(task.FailureReason).Should(Equal("because i said so"))

				node, err := store.Get("/v1/run_once/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err := bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.CompleteTask(task, false, "", "a result")
				})
			})
		})

		Context("When completing a Task that is not in the running state", func() {
			It("returns an error", func() {
				err := bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("MaintainExecutorPresence", func() {
		var (
			executorId string
			interval   time.Duration
			status     <-chan bool
			reporter   *test_helpers.StatusReporter
			err        error
			presence   Presence
		)

		BeforeEach(func() {
			executorId = "stubExecutor"
			interval = 1 * time.Second

			presence, status, err = bbs.MaintainExecutorPresence(interval, executorId)
			Ω(err).ShouldNot(HaveOccurred())

			reporter = maintainStatus(status)
		})

		AfterEach(func() {
			presence.Remove()
		})

		It("should put /executor/EXECUTOR_ID in the store with a TTL", func() {
			Eventually(reporter.Locked).Should(BeTrue())

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

			presenceA, statusA, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-a")
			Ω(err).ShouldNot(HaveOccurred())
			maintainStatus(statusA)

			presenceB, statusB, err := bbs.MaintainExecutorPresence(1*time.Second, "executor-b")
			Ω(err).ShouldNot(HaveOccurred())
			maintainStatus(statusB)

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

	Describe("WatchForDesiredTask", func() {
		var (
			events <-chan *models.Task
			stop   chan<- bool
			errors <-chan error
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForDesiredTask()
		})

		It("should send an event down the pipe for creates", func(done Done) {
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			close(done)
		})

		It("should send an event down the pipe for sets", func(done Done) {
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			e := <-events

			Expect(e).To(Equal(task))

			err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			err = bbs.ResolveTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			otherTask := task
			otherTask.Guid = task.Guid + "1"

			err = bbs.DesireTask(otherTask)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherTask))

			close(done)
		})

		It("closes the events and errors channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())

			close(done)
		})
	})

	Describe("ConvergeTask", func() {
		var desiredEvents <-chan *models.Task
		var completedEvents <-chan *models.Task

		commenceWatching := func() {
			desiredEvents, _, _ = bbs.WatchForDesiredTask()
			completedEvents, _, _ = bbs.WatchForCompletedTask()
		}

		Context("when a Task is malformed", func() {
			It("should delete it", func() {
				nodeKey := path.Join(TaskSchemaRoot, "some-guid")

				err := store.Create(storeadapter.StoreNode{
					Key:   nodeKey,
					Value: []byte("ß"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = store.Get(nodeKey)
				Ω(err).ShouldNot(HaveOccurred())

				bbs.ConvergeTask(timeToClaim)

				_, err = store.Get(nodeKey)
				Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when a Task is pending", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should kick the Task", func() {
				timeProvider.IncrementBySeconds(1)
				commenceWatching()
				bbs.ConvergeTask(timeToClaim)

				var noticedOnce *models.Task
				Eventually(desiredEvents).Should(Receive(&noticedOnce))

				task.UpdatedAt = timeProvider.Time().UnixNano()
				Ω(noticedOnce).Should(Equal(task))
			})

			Context("when the Task has been pending for longer than the timeToClaim", func() {
				It("should mark the Task as completed & failed", func() {
					timeProvider.IncrementBySeconds(31)
					commenceWatching()
					bbs.ConvergeTask(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("time limit"))
				})
			})
		})

		Context("when a Task is claimed", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				var status <-chan bool
				presence, status, err = bbs.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
				maintainStatus(status)
			})

			AfterEach(func() {
				presence.Remove()
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been claimed for > 30 seconds", func() {
				It("should mark the Task as pending", func() {
					timeProvider.IncrementBySeconds(30)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim)

					Consistently(completedEvents).ShouldNot(Receive())

					var noticedOnce *models.Task
					Eventually(desiredEvents).Should(Receive(&noticedOnce))

					task.State = models.TaskStatePending
					task.UpdatedAt = timeProvider.Time().UnixNano()
					task.ExecutorID = ""
					Ω(noticedOnce).Should(Equal(task))
				})
			})

			Context("when the associated executor is missing", func() {
				BeforeEach(func() {
					presence.Remove()
				})

				It("should mark the Task as completed & failed", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a Task is running", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				var status <-chan bool
				presence, status, err = bbs.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
				maintainStatus(status)
			})

			AfterEach(func() {
				presence.Remove()
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the associated executor is missing", func() {
				BeforeEach(func() {
					presence.Remove()
				})

				It("should mark the Task as completed & failed", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce *models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a Task is completed", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task, true, "'cause I said so", "a magical result")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should kick the Task", func() {
				timeProvider.IncrementBySeconds(1)
				commenceWatching()

				bbs.ConvergeTask(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())

				var noticedOnce *models.Task
				Eventually(completedEvents).Should(Receive(&noticedOnce))

				Ω(noticedOnce.Failed).Should(Equal(true))
				Ω(noticedOnce.FailureReason).Should(Equal("'cause I said so"))
				Ω(noticedOnce.Result).Should(Equal("a magical result"))
				Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})

		Context("when a Task is resolving", func() {
			BeforeEach(func() {
				err := bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task, true, "'cause I said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ResolvingTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been resolving for > 30 seconds", func() {
				It("should put the Task back into the completed state", func() {
					timeProvider.IncrementBySeconds(30)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim)

					var noticedOnce *models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					task.State = models.TaskStateCompleted
					task.UpdatedAt = timeProvider.Time().UnixNano()
					Ω(noticedOnce).Should(Equal(task))
				})
			})
		})
	})

	Context("MaintainConvergeLock", func() {
		Context("when the lock is available", func() {
			It("should return immediately", func() {
				status, releaseLock, err := bbs.MaintainConvergeLock(1*time.Minute, "id")
				Ω(err).ShouldNot(HaveOccurred())

				defer close(releaseLock)

				Ω(status).ShouldNot(BeNil())
				Ω(releaseLock).ShouldNot(BeNil())

				reporter := maintainStatus(status)
				Eventually(reporter.Locked).Should(Equal(true))
			})

			It("should maintain the lock in the background", func() {
				interval := 1 * time.Second

				status, releaseLock, err := bbs.MaintainConvergeLock(interval, "id_1")
				Ω(err).ShouldNot(HaveOccurred())

				defer close(releaseLock)

				reporter1 := test_helpers.NewStatusReporter(status)
				Eventually(reporter1.Locked).Should(BeTrue())

				status2, releaseLock2, err2 := bbs.MaintainConvergeLock(interval, "id_2")
				Ω(err2).ShouldNot(HaveOccurred())

				defer close(releaseLock2)

				reporter2 := test_helpers.NewStatusReporter(status2)
				Consistently(reporter2.Locked, (interval * 2).Seconds()).Should(BeFalse())
			})

			Context("when the lock disappears after it has been acquired (e.g. ETCD store is reset)", func() {
				It("should send a notification down the lostLockChannel", func() {
					status, releaseLock, err := bbs.MaintainConvergeLock(1*time.Second, "id")
					Ω(err).ShouldNot(HaveOccurred())

					reporter := test_helpers.NewStatusReporter(status)

					Eventually(reporter.Locked).Should(BeTrue())

					etcdRunner.Stop()

					Eventually(reporter.Locked).Should(BeFalse())

					releaseLock <- nil
				})
			})
		})

		Context("when releasing the lock", func() {
			It("makes it available for others trying to acquire it", func() {
				interval := 1 * time.Second

				status, release, err := bbs.MaintainConvergeLock(interval, "my_id")
				Ω(err).ShouldNot(HaveOccurred())

				reporter1 := test_helpers.NewStatusReporter(status)
				Eventually(reporter1.Locked, (interval * 2).Seconds()).Should(BeTrue())

				status2, release2, err2 := bbs.MaintainConvergeLock(interval, "other_id")
				Ω(err2).ShouldNot(HaveOccurred())

				reporter2 := test_helpers.NewStatusReporter(status2)
				Consistently(reporter2.Locked, (interval * 2).Seconds()).Should(BeFalse())

				Ω(reporter1.Reporting()).Should(BeTrue())

				release <- nil

				Eventually(reporter1.Reporting).Should(BeFalse())

				Eventually(reporter2.Locked, (interval * 2).Seconds()).Should(BeTrue())

				release2 <- nil

				Eventually(reporter2.Reporting).Should(BeFalse())
			})
		})
	})
})

func maintainStatus(status <-chan bool) *test_helpers.StatusReporter {
	return test_helpers.NewStatusReporter(status)
}
