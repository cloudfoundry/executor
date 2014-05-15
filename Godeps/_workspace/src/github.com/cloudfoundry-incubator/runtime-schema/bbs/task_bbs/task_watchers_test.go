package task_bbs_test

import (
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
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
		bbs = New(etcdClient, timeProvider)
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

			bbs.ConvergeTask(time.Second)

			Expect(<-events).To(Equal(task))

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

		It("closes the events and errors channel when told to stop", func(done Done) {
			stop <- true

			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())

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
