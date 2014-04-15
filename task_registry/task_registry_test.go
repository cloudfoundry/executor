package task_registry_test

import (
	. "github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskRegistry", func() {
	var taskRegistry *TaskRegistry
	var runOnce *models.RunOnce

	BeforeEach(func() {
		runOnce = &models.RunOnce{
			MemoryMB: 255,
			DiskMB:   1023,
			Guid:     "a guid",
			Stack:    "some-stack",
		}

		taskRegistry = NewTaskRegistry("some-stack", 256, 1024)
	})

	Describe("AddRunOnce", func() {
		It("adds something to the registry when there are enough resources", func() {
			err := taskRegistry.AddRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(taskRegistry.RunOnces[runOnce.Guid]).To(Equal(runOnce))
		})

		Context("when the RunOnce's stack is incompatible", func() {
			BeforeEach(func() {
				runOnceWithInvalidStack := runOnce
				runOnceWithInvalidStack.Stack = "invalid"

				runOnce = runOnceWithInvalidStack
			})

			It("rejects the RunOnce", func() {
				err := taskRegistry.AddRunOnce(runOnce)
				Ω(err).Should(Equal(IncompatibleStackError{"some-stack", "invalid"}))
			})
		})

		Context("when not configured with a stack", func() {
			BeforeEach(func() {
				runOnceWithNoStack := runOnce
				runOnceWithNoStack.Stack = ""

				runOnce = runOnceWithNoStack
			})

			It("rejects the RunOnce", func() {
				err := taskRegistry.AddRunOnce(runOnce)
				Ω(err).Should(Equal(ErrorNoStackDefined))
			})
		})

		Context("when there aren't enough resources", func() {
			BeforeEach(func() {
				err := taskRegistry.AddRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})

			Context("for the task's memory", func() {
				It("returns an error", func() {
					err := taskRegistry.AddRunOnce(&models.RunOnce{
						MemoryMB: 2,
					})
					Ω(err).Should(HaveOccurred())
					Ω(taskRegistry.RunOnces).To(HaveLen(1))
				})
			})

			Context("for the task's disk", func() {
				It("returns an error", func() {
					err := taskRegistry.AddRunOnce(&models.RunOnce{
						DiskMB: 2,
					})
					Ω(err).Should(HaveOccurred())
					Ω(taskRegistry.RunOnces).To(HaveLen(1))
				})
			})
		})
	})

	Describe("RemoveRunOnce", func() {
		BeforeEach(func() {
			err := taskRegistry.AddRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("reclaims the disk and memory from the RunOnce", func() {
			taskRegistry.RemoveRunOnce(runOnce)

			err := taskRegistry.AddRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})
	})
})
