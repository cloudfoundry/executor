package bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("Stager BBS", func() {
	var bbs *BBS
	var task models.Task
	var timeProvider *faketimeprovider.FakeTimeProvider
	var err error

	BeforeEach(func() {
		err = nil
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))

		bbs = New(etcdClient, timeProvider)
		task = models.Task{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
		}
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

	Describe("WatchForCompletedTask", func() {
		var (
			events <-chan models.Task
			stop   chan<- bool
			errors <-chan error
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForCompletedTask()

			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.StartTask(task, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should not send any events for state transitions that we are not interested in", func() {
			Consistently(events).ShouldNot(Receive())
		})

		It("should send an event down the pipe for completed run onces", func(done Done) {
			task, err = bbs.CompleteTask(task, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			close(done)
		})

		It("should not send an event down the pipe when resolved", func(done Done) {
			task, err = bbs.CompleteTask(task, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			task, err = bbs.ResolvingTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ResolveTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Consistently(events).ShouldNot(Receive())

			close(done)
		})

		It("closes the events and errorschannel when told to stop", func(done Done) {
			stop <- true

			task, err = bbs.CompleteTask(task, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())

			close(done)
		})
	})
})
