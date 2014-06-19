package task_bbs_test

import (
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
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

	Describe("WatchForDesiredTask", func() {
		var (
			events <-chan models.Task
			stop   chan<- bool
			errors <-chan error
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForDesiredTask()
		})

		AfterEach(func() {
			stop <- true
		})

		It("should send an event down the pipe for creates", func() {
			err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			var receivedTask models.Task
			Eventually(events).Should(Receive(&receivedTask))

			Ω(receivedTask.Guid).Should(Equal(task.Guid))
			Ω(receivedTask.State).Should(Equal(models.TaskStatePending))
		})

		It("should send an event down the pipe when the converge is run", func() {
			err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			timeProvider.IncrementBySeconds(2)
			bbs.ConvergeTask(5*time.Second, time.Second)

			Eventually(events).Should(Receive())
		})

		It("should not send an event down the pipe for deletes", func() {
			err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			err = bbs.ClaimTask(task.Guid, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.CompleteTask(task.Guid, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ResolvingTask(task.Guid)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ResolveTask(task.Guid)
			Ω(err).ShouldNot(HaveOccurred())

			Consistently(events).ShouldNot(Receive())
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

			err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimTask(task.Guid, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartTask(task.Guid, "executor-ID", "container-handle")
			Ω(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			stop <- true
		})

		It("should not send any events for state transitions that we are not interested in", func() {
			Consistently(events).ShouldNot(Receive())
		})

		It("should send an event down the pipe for completed run onces", func() {
			err = bbs.CompleteTask(task.Guid, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			var receivedTask models.Task
			Eventually(events).Should(Receive(&receivedTask))
			Ω(receivedTask.Guid).Should(Equal(task.Guid))
			Ω(receivedTask.State).Should(Equal(models.TaskStateCompleted))
			Ω(receivedTask.FailureReason).Should(Equal("a reason"))
		})

		It("should not send an event down the pipe when resolved", func() {
			err = bbs.CompleteTask(task.Guid, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive())

			err = bbs.ResolvingTask(task.Guid)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ResolveTask(task.Guid)
			Ω(err).ShouldNot(HaveOccurred())

			Consistently(events).ShouldNot(Receive())
		})
	})
})
