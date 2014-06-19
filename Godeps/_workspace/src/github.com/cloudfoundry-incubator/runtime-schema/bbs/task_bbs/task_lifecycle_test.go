package task_bbs_test

import (
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Task BBS", func() {
	var bbs *TaskBBS
	var task models.Task
	var timeProvider *faketimeprovider.FakeTimeProvider
	var err error

	BeforeEach(func() {
		err = nil
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
	})

	Describe("DesireTask", func() {
		Context("when the Task has a CreatedAt time", func() {
			BeforeEach(func() {
				task.CreatedAt = 1234812
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("creates /task/<guid>", func() {
				tasks, err := bbs.GetAllPendingTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].Guid).Should(Equal(task.Guid))
				Ω(tasks[0].CreatedAt).Should(Equal(task.CreatedAt))
				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})

		Context("when the Task has no CreatedAt time", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("creates /task/<guid> and sets set the CreatedAt time to now", func() {
				tasks, err := bbs.GetAllPendingTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].CreatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			It("should bump UpdatedAt", func() {
				tasks, err := bbs.GetAllPendingTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})

		Context("Common cases", func() {
			Context("when the Task is already pending", func() {
				It("returns an error", func() {
					err = bbs.DesireTask(task)
					Ω(err).ShouldNot(HaveOccurred())

					err = bbs.DesireTask(task)
					Ω(err).Should(HaveOccurred())
				})
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.DesireTask(task)
				})
			})

			It("bumps UpdatedAt", func() {
				err = bbs.DesireTask(task)

				tasks, err := bbs.GetAllPendingTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})
	})

	Describe("ClaimTask", func() {
		Context("when claiming a pending Task", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("puts the Task in the claim state", func() {
				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllClaimedTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].Guid).Should(Equal(task.Guid))
				Ω(tasks[0].State).Should(Equal(models.TaskStateClaimed))
				Ω(tasks[0].ExecutorID).Should(Equal("executor-ID"))
			})

			It("should bump UpdatedAt", func() {
				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllClaimedTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the etcdClient is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ClaimTask(task.Guid, "executor-ID")
				})
			})
		})

		Context("when claiming an already-claimed task", func() {
			It("returns an error", func() {
				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when claiming a Task that is not in the pending state", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns an error", func() {
				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).Should(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "some-other-executor-ID")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("StartTask", func() {
		Context("when starting a claimed Task", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the state to running", func() {
				err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllRunningTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].Guid).Should(Equal(task.Guid))
				Ω(tasks[0].State).Should(Equal(models.TaskStateRunning))
				Ω(tasks[0].ContainerHandle).Should(Equal("container-handle"))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllRunningTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.StartTask(task.Guid, "executor-ID", "container-handle")
				})
			})
		})

		Context("When starting a Task that is not in the claimed state", func() {
			It("returns an error", func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("When starting a Task that was claimed by a different executor", func() {
			It("returns an error", func() {
				err = bbs.StartTask(task.Guid, "some-other-executor-ID", "container-handle")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("CompleteTask", func() {
		Context("when completing a running Task", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the Task in the completed state", func() {
				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllCompletedTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].Failed).Should(BeTrue())
				Ω(tasks[0].FailureReason).Should(Equal("because i said so"))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllCompletedTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.CompleteTask(task.Guid, false, "", "a result")
				})
			})
		})

		Context("When completing a Task that is not in the running state", func() {
			It("returns an error", func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).Should(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("ResolvingTask", func() {
		Context("when the task is complete", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "some-executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "some-executor-id", "some-container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("swaps /task/<guid>'s state to resolving", func() {
				err = bbs.ResolvingTask(task.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllResolvingTasks()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(tasks[0].Guid).Should(Equal(task.Guid))
				Ω(tasks[0].State).Should(Equal(models.TaskStateResolving))
			})

			It("bumps UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				err = bbs.ResolvingTask(task.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllResolvingTasks()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the Task is already resolving", func() {
				BeforeEach(func() {
					err = bbs.ResolvingTask(task.Guid)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("fails", func() {
					err = bbs.ResolvingTask(task.Guid)
					Ω(err).Should(HaveOccurred())
				})
			})
		})

		Context("when the task is not complete", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "some-executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "some-executor-id", "some-container-handle")
				Ω(err).ShouldNot(HaveOccurred())

			})

			It("should fail", func() {
				err := bbs.ResolvingTask(task.Guid)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("ResolveTask", func() {
		Context("when the task is resolving", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "some-executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "some-executor-id", "some-container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ResolvingTask(task.Guid)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should remove /task/<guid>", func() {
				err = bbs.ResolveTask(task.Guid)
				Ω(err).ShouldNot(HaveOccurred())

				tasks, err := bbs.GetAllTasks()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(tasks).Should(BeEmpty())
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ResolveTask(task.Guid)
				})
			})
		})

		Context("when the task is not resolving", func() {
			BeforeEach(func() {
				err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.ClaimTask(task.Guid, "some-executor-id")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartTask(task.Guid, "some-executor-id", "some-container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteTask(task.Guid, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should fail", func() {
				err = bbs.ResolveTask(task.Guid)
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
