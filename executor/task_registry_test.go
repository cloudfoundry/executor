package executor_test

import (
	. "github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskRegistry", func() {
	var taskRegistry *TaskRegistry
	var runOnce models.RunOnce

	BeforeEach(func() {
		runOnce = models.RunOnce{
			MemoryMB: 256,
			DiskMB:   1024,
		}
		taskRegistry = NewTaskRegistry(256, 1024)
	})

	Describe("HasCapacityForRunOnce", func() {
		It("Returns true when there are enough resources", func() {
			Ω(taskRegistry.HasCapacityForRunOnce(runOnce)).To(BeTrue())
		})

		It("Returns false when it doesn't have enough memory", func() {
			taskRegistry.AddRunOnce(models.RunOnce{
				MemoryMB: 1,
			})

			Ω(taskRegistry.HasCapacityForRunOnce(runOnce)).To(BeFalse())
		})

		It("Returns false when it doesn't have enough disk", func() {
			taskRegistry.AddRunOnce(models.RunOnce{
				DiskMB: 1,
			})

			Ω(taskRegistry.HasCapacityForRunOnce(runOnce)).To(BeFalse())
		})

	})

	Describe("AddRunOnce", func() {
		It("Adds a RunOnce to the registry", func() {
			taskRegistry.AddRunOnce(runOnce)
			Ω(taskRegistry.RunOnces()[runOnce.Guid]).To(Equal(runOnce))
		})
	})
})
