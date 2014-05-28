package task_bbs_test

import (
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
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

	Describe("ClaimTask", func() {
		Context("when claiming a pending Task", func() {
			BeforeEach(func() {
				task, err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("puts the Task in the claim state", func() {
				task, err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.State).Should(Equal(models.TaskStateClaimed))
				Ω(task.ExecutorID).Should(Equal("executor-ID"))

				node, err := etcdClient.Get("/v1/task/some-guid")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
					Key:   "/v1/task/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				task, err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the etcdClient is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					_, err := bbs.ClaimTask(task, "executor-ID")
					return err
				})
			})
		})

		Context("when claiming a Task that is not in the pending state", func() {
			It("returns an error", func() {
				task, err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("StartTask", func() {
		Context("when starting a claimed Task", func() {
			BeforeEach(func() {
				task, err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				task, err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the state to running", func() {
				task, err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.State).Should(Equal(models.TaskStateRunning))
				Ω(task.ContainerHandle).Should(Equal("container-handle"))

				node, err := etcdClient.Get("/v1/task/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
					Key:   "/v1/task/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				task, err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					_, err := bbs.StartTask(task, "container-handle")
					return err
				})
			})
		})

		Context("When starting a Task that is not in the claimed state", func() {
			It("returns an error", func() {
				task, err = bbs.StartTask(task, "container-handle")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("CompleteTask", func() {
		Context("when completing a running Task", func() {
			BeforeEach(func() {
				task, err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				task, err = bbs.ClaimTask(task, "executor-ID")
				Ω(err).ShouldNot(HaveOccurred())

				task, err = bbs.StartTask(task, "container-handle")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("sets the Task in the completed state", func() {
				task, err = bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.Failed).Should(BeTrue())
				Ω(task.FailureReason).Should(Equal("because i said so"))

				node, err := etcdClient.Get("/v1/task/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
					Key:   "/v1/task/some-guid",
					Value: task.ToJSON(),
				}))
			})

			It("should bump UpdatedAt", func() {
				timeProvider.IncrementBySeconds(1)

				task, err = bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					_, err := bbs.CompleteTask(task, false, "", "a result")
					return err
				})
			})
		})

		Context("When completing a Task that is not in the running state", func() {
			It("returns an error", func() {
				_, err := bbs.CompleteTask(task, true, "because i said so", "a result")
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("DesireTask", func() {
		Context("when the Task has a CreatedAt time", func() {
			BeforeEach(func() {
				task.CreatedAt = 1234812
				task, err = bbs.DesireTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("creates /task/<guid>", func() {
				node, err := etcdClient.Get("/v1/task/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.Value).Should(Equal(task.ToJSON()))
			})
		})

		Context("when the Task has no CreatedAt time", func() {
			BeforeEach(func() {
				task, err = bbs.DesireTask(task)
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
					task, err = bbs.DesireTask(task)
					Ω(err).ShouldNot(HaveOccurred())

					task, err = bbs.DesireTask(task)
					Ω(err).Should(HaveOccurred())
				})
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					_, err := bbs.DesireTask(task)
					return err
				})
			})

			It("bumps UpdatedAt", func() {
				task, err = bbs.DesireTask(task)

				tasks, err := bbs.GetAllPendingTasks()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(tasks[0].UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
			})
		})
	})

	Describe("ResolvingTask", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "some-executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.StartTask(task, "some-container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.CompleteTask(task, true, "because i said so", "a result")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("swaps /task/<guid>'s state to resolving", func() {
			task, err = bbs.ResolvingTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(task.State).Should(Equal(models.TaskStateResolving))

			node, err := etcdClient.Get("/v1/task/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.Value).Should(Equal(task.ToJSON()))
		})

		It("bumps UpdatedAt", func() {
			timeProvider.IncrementBySeconds(1)

			task, err = bbs.ResolvingTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(task.UpdatedAt).Should(Equal(timeProvider.Time().UnixNano()))
		})

		Context("when the Task is already resolving", func() {
			BeforeEach(func() {
				task, err = bbs.ResolvingTask(task)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("fails", func() {
				task, err = bbs.ResolvingTask(task)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(task.State).Should(Equal(models.TaskStateResolving))

				node, err := etcdClient.Get("/v1/task/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node.Value).Should(Equal(task.ToJSON()))
			})
		})
	})

	Describe("ResolveTask", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "some-executor-id")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.StartTask(task, "some-container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.CompleteTask(task, true, "because i said so", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ResolvingTask(task)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should remove /task/<guid>", func() {
			task, err = bbs.ResolveTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			_, err = etcdClient.Get("/v1/task/some-guid")
			Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				_, err := bbs.ResolveTask(task)
				return err
			})
		})
	})
})
