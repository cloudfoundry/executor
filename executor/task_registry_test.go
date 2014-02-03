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
			MemoryMB: 255,
			DiskMB:   1023,
		}
		taskRegistry = NewTaskRegistry(256, 1024)
	})

	Describe("AddRunOnce", func() {
		It("Returns true and adds something to the registry when there are enough resources", func() {
			Ω(taskRegistry.AddRunOnce(runOnce)).To(BeTrue())
			Ω(taskRegistry.RunOnces()[runOnce.Guid]).To(Equal(runOnce))
		})

		Context("When there aren't enough resources", func() {
			BeforeEach(func() {
				taskRegistry.AddRunOnce(runOnce)
				Ω(taskRegistry.RunOnces()).To(HaveLen(1))
			})

			It("Returns false and adds nothing when the new RunOnce needs more memory than is available", func() {
				Ω(taskRegistry.AddRunOnce(models.RunOnce{
					MemoryMB: 2,
				})).To(BeFalse())
				Ω(taskRegistry.RunOnces()).To(HaveLen(1))
			})

			It("Returns false and adds nothing when the new RunOnce needs more disk than is available", func() {
				Ω(taskRegistry.AddRunOnce(models.RunOnce{
					DiskMB: 2,
				})).To(BeFalse())
				Ω(taskRegistry.RunOnces()).To(HaveLen(1))
			})
		})
	})
})
