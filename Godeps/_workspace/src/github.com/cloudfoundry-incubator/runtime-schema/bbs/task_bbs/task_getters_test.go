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

	Describe("GetAllPendingTasks", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all Tasks in 'pending' state", func() {
			tasks, err := bbs.GetAllPendingTasks()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(tasks).Should(HaveLen(1))
			Ω(tasks).Should(ContainElement(task))
		})
	})

	Describe("GetAllClaimedTasks", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all Tasks in 'claimed' state", func() {
			tasks, err := bbs.GetAllClaimedTasks()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(tasks).Should(HaveLen(1))
			Ω(tasks).Should(ContainElement(task))
		})
	})

	Describe("GetAllStartingTasks", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.StartTask(task, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all Tasks in 'running' state", func() {
			tasks, err := bbs.GetAllStartingTasks()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(tasks).Should(HaveLen(1))
			Ω(tasks).Should(ContainElement(task))
		})
	})

	Describe("GetAllCompletedTasks", func() {
		BeforeEach(func() {
			task, err = bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.StartTask(task, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			task, err = bbs.CompleteTask(task, true, "a reason", "a result")
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns all Tasks in 'completed' state", func() {
			tasks, err := bbs.GetAllCompletedTasks()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(tasks).Should(HaveLen(1))
			Ω(tasks).Should(ContainElement(task))
		})
	})
})
