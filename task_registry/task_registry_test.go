package task_registry_test

import (
	. "github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskRegistry", func() {
	var taskRegistry *TaskRegistry
	var runOnce *models.Task

	BeforeEach(func() {
		runOnce = &models.Task{
			MemoryMB: 255,
			DiskMB:   1023,
			Guid:     "a guid",
			Stack:    "some-stack",
		}

		taskRegistry = NewTaskRegistry("some-stack", 256, 1024)
	})

	Describe("AddTask", func() {
		It("adds something to the registry when there are enough resources", func() {
			err := taskRegistry.AddTask(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(taskRegistry.Tasks[runOnce.Guid]).To(Equal(runOnce))
		})

		Context("when the Task's stack is incompatible", func() {
			BeforeEach(func() {
				runOnceWithInvalidStack := runOnce
				runOnceWithInvalidStack.Stack = "invalid"

				runOnce = runOnceWithInvalidStack
			})

			It("rejects the Task", func() {
				err := taskRegistry.AddTask(runOnce)
				Ω(err).Should(Equal(IncompatibleStackError{"some-stack", "invalid"}))
			})
		})

		Context("when not configured with a stack", func() {
			BeforeEach(func() {
				runOnceWithNoStack := runOnce
				runOnceWithNoStack.Stack = ""

				runOnce = runOnceWithNoStack
			})

			It("rejects the Task", func() {
				err := taskRegistry.AddTask(runOnce)
				Ω(err).Should(Equal(ErrorNoStackDefined))
			})
		})

		Context("when there aren't enough resources", func() {
			BeforeEach(func() {
				err := taskRegistry.AddTask(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(taskRegistry.Tasks).To(HaveLen(1))
			})

			Context("for the task's memory", func() {
				It("returns an error", func() {
					err := taskRegistry.AddTask(&models.Task{
						MemoryMB: 2,
					})
					Ω(err).Should(HaveOccurred())
					Ω(taskRegistry.Tasks).To(HaveLen(1))
				})
			})

			Context("for the task's disk", func() {
				It("returns an error", func() {
					err := taskRegistry.AddTask(&models.Task{
						DiskMB: 2,
					})
					Ω(err).Should(HaveOccurred())
					Ω(taskRegistry.Tasks).To(HaveLen(1))
				})
			})
		})
	})

	Describe("RemoveTask", func() {
		BeforeEach(func() {
			err := taskRegistry.AddTask(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("reclaims the disk and memory from the Task", func() {
			taskRegistry.RemoveTask(runOnce)

			err := taskRegistry.AddTask(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})
	})
})
