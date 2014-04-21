package bbs_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
)

var _ = Describe("Task BBS", func() {
	var bbs *BBS
	var task *models.Task
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
		task = &models.Task{
			Guid:      "some-guid",
			CreatedAt: time.Now().UnixNano(),
		}
	})

	Describe("GetAllPendingTasks", func() {
		BeforeEach(func() {
			err := bbs.DesireTask(task)
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
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimTask(task, "executor-ID")
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
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartTask(task, "container-handle")
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
			err := bbs.DesireTask(task)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimTask(task, "executor-ID")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartTask(task, "container-handle")
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.CompleteTask(task, true, "a reason", "a result")
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
