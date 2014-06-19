package task_bbs_test

import (
	"path"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/test_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Convergence of Tasks", func() {
	var bbs *TaskBBS
	var task models.Task
	var timeToClaimInSeconds, convergenceIntervalInSeconds uint64
	var timeToClaim, convergenceInterval time.Duration
	var timeProvider *faketimeprovider.FakeTimeProvider
	var err error
	var servicesBBS *services_bbs.ServicesBBS
	var presence services_bbs.Presence

	BeforeEach(func() {
		err = nil

		timeToClaimInSeconds = 30
		timeToClaim = time.Duration(timeToClaimInSeconds) * time.Second
		convergenceIntervalInSeconds = 10
		convergenceInterval = time.Duration(convergenceIntervalInSeconds) * time.Second

		timeProvider = faketimeprovider.New(time.Unix(1238, 0))

		logSink := steno.NewTestingSink()

		steno.Init(&steno.Config{
			Sinks: []steno.Sink{logSink},
		})

		logger := steno.NewLogger("the-logger")
		steno.EnterTestMode()

		bbs = New(etcdClient, timeProvider, logger)
		task = models.Task{
			Guid: "some-guid",
		}
		servicesBBS = services_bbs.New(etcdClient, logger)
	})

	Describe("ConvergeTask", func() {
		var desiredEvents <-chan models.Task
		var completedEvents <-chan models.Task

		commenceWatching := func() {
			desiredEvents, _, _ = bbs.WatchForDesiredTask()
			completedEvents, _, _ = bbs.WatchForCompletedTask()
		}

		Context("when a Task is malformed", func() {
			It("should delete it", func() {
				nodeKey := path.Join(shared.TaskSchemaRoot, "some-guid")

				err := etcdClient.Create(storeadapter.StoreNode{
					Key:   nodeKey,
					Value: []byte("ß"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, err = etcdClient.Get(nodeKey)
				Ω(err).ShouldNot(HaveOccurred())

				bbs.ConvergeTask(timeToClaim, convergenceInterval)

				_, err = etcdClient.Get(nodeKey)
				Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
			})
		})

		Context("when a Task is pending", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when the Task has *not* been pending for too long", func() {
				It("should not kick the Task", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()
					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())
				})
			})

			Context("when the Task has been pending for longer than the convergence interval", func() {
				It("should kick the Task", func() {
					timeProvider.IncrementBySeconds(convergenceIntervalInSeconds + 1)
					commenceWatching()
					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					var noticedOnce models.Task
					Eventually(desiredEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Guid).Should(Equal(task.Guid))
					Ω(noticedOnce.State).Should(Equal(models.TaskStatePending))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})

			Context("when the Task has been pending for longer than the timeToClaim", func() {
				It("should mark the Task as completed & failed", func() {
					timeProvider.IncrementBySeconds(timeToClaimInSeconds + 1)
					commenceWatching()
					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("time limit"))
				})
			})
		})

		Context("when a Task is claimed", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				var status <-chan bool
				presence, status, err = servicesBBS.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
				test_helpers.NewStatusReporter(status)
			})

			AfterEach(func() {
				presence.Remove()
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim, convergenceInterval)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been claimed for > 30 seconds", func() {
				It("should mark the Task as pending", func() {
					timeProvider.IncrementBySeconds(convergenceIntervalInSeconds)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(completedEvents).ShouldNot(Receive())

					var noticedOnce models.Task
					Eventually(desiredEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Guid).Should(Equal(task.Guid))
					Ω(noticedOnce.State).Should(Equal(models.TaskStatePending))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
					Ω(noticedOnce.ExecutorID).Should(BeEmpty())
				})
			})

			Context("when the associated executor is missing", func() {
				BeforeEach(func() {
					presence.Remove()
				})

				It("should mark the Task as completed & failed", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a Task is running", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "executor-id", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				var status <-chan bool
				presence, status, err = servicesBBS.MaintainExecutorPresence(time.Minute, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())
				test_helpers.NewStatusReporter(status)
			})

			AfterEach(func() {
				presence.Remove()
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim, convergenceInterval)

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

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(ContainSubstring("executor"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})

		Context("when a Task is completed", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "executor-id", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "'cause I said so", "a magical result")
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("when the task has been completed for > the convergence interval", func() {
				It("should kick the Task", func() {
					timeProvider.IncrementBySeconds(convergenceIntervalInSeconds + 1)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())

					var noticedOnce models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Failed).Should(Equal(true))
					Ω(noticedOnce.FailureReason).Should(Equal("'cause I said so"))
					Ω(noticedOnce.Result).Should(Equal("a magical result"))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})

			Context("when the task has been completed for < the convergence interval", func() {
				It("should kick the Task", func() {
					timeProvider.IncrementBySeconds(1)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					Consistently(desiredEvents).ShouldNot(Receive())
					Consistently(completedEvents).ShouldNot(Receive())
				})
			})
		})

		Context("when a Task is resolving", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "executor-id", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "'cause I said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ResolvingTask(task.Guid)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should do nothing", func() {
				commenceWatching()

				bbs.ConvergeTask(timeToClaim, convergenceInterval)

				Consistently(desiredEvents).ShouldNot(Receive())
				Consistently(completedEvents).ShouldNot(Receive())
			})

			Context("when the run once has been resolving for > 30 seconds", func() {
				It("should put the Task back into the completed state", func() {
					timeProvider.IncrementBySeconds(convergenceIntervalInSeconds)
					commenceWatching()

					bbs.ConvergeTask(timeToClaim, convergenceInterval)

					var noticedOnce models.Task
					Eventually(completedEvents).Should(Receive(&noticedOnce))

					Ω(noticedOnce.Guid).Should(Equal(task.Guid))
					Ω(noticedOnce.State).Should(Equal(models.TaskStateCompleted))
					Ω(noticedOnce.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
				})
			})
		})
	})
})
