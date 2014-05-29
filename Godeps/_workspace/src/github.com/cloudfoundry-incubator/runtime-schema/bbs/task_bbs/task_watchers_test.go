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

		It("should send an event down the pipe for creates", func(done Done) {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			close(done)
		})

		It("should send an event down the pipe when the converge is run", func(done Done) {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			e := <-events

			Expect(e).To(Equal(task))

			timeProvider.IncrementBySeconds(2)
			bbs.ConvergeTask(5*time.Second, time.Second)

			Eventually(events).Should(Receive())

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(task))

			task, err = bbs.ResolveTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			otherTask := task
			otherTask.Guid = task.Guid + "1"

			otherTask, err = bbs.DesireTask(otherTask)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherTask))

			close(done)
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

		AfterEach(func() {
			stop <- true
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
	})
})
