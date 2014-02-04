package executor_test

import (
	"io/ioutil"
	"os"

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
			Guid:     "I be totally yooniq, yo",
		}
		taskRegistry = NewTaskRegistry(256, 1024)
	})

	Describe("AddRunOnce", func() {
		It("Returns true and adds something to the registry when there are enough resources", func() {
			Ω(taskRegistry.AddRunOnce(runOnce)).To(BeTrue())
			Ω(taskRegistry.RunOnces[runOnce.Guid]).To(Equal(runOnce))
		})

		Context("When there aren't enough resources", func() {
			BeforeEach(func() {
				taskRegistry.AddRunOnce(runOnce)
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})

			It("Returns false and adds nothing when the new RunOnce needs more memory than is available", func() {
				Ω(taskRegistry.AddRunOnce(models.RunOnce{
					MemoryMB: 2,
				})).To(BeFalse())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})

			It("Returns false and adds nothing when the new RunOnce needs more disk than is available", func() {
				Ω(taskRegistry.AddRunOnce(models.RunOnce{
					DiskMB: 2,
				})).To(BeFalse())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})
		})
	})

	Describe("RemoveRunOnce", func() {
		BeforeEach(func() {
			taskRegistry.AddRunOnce(runOnce)
		})

		It("should reclaim the disk and memory from the RunOnce", func() {
			originalMemory := taskRegistry.AvailableMemoryMB()
			originalDisk := taskRegistry.AvailableDiskMB()

			taskRegistry.RemoveRunOnce(runOnce)

			Ω(taskRegistry.AvailableMemoryMB()).To(Equal(originalMemory + runOnce.MemoryMB))
			Ω(taskRegistry.AvailableDiskMB()).To(Equal(originalDisk + runOnce.DiskMB))
		})
	})

	Describe("LoadTaskRegistryFromDisk", func() {
		var runOnce models.RunOnce
		var diskRegistry *TaskRegistry

		AfterEach(func() {
			os.Remove("saved_registry")
		})

		Context("When there is a valid task registry on disk", func() {
			BeforeEach(func() {
				diskRegistry = NewTaskRegistry(512, 2048)
				runOnce = models.RunOnce{
					Guid:     "a guid",
					MemoryMB: 256,
					DiskMB:   1024,
				}
				diskRegistry.AddRunOnce(runOnce)
				diskRegistry.WriteToDisk()
			})

			It("should load up the task registry and return it", func() {
				loadedTaskRegistry, err := LoadTaskRegistryFromDisk(512, 2048)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(loadedTaskRegistry.RunOnces).To(HaveLen(1))
				Ω(loadedTaskRegistry.RunOnces["a guid"]).To(Equal(runOnce))

				addedRunOnce := loadedTaskRegistry.AddRunOnce(models.RunOnce{Guid: "another guid", MemoryMB: 1, DiskMB: 1})
				Ω(addedRunOnce).To(BeTrue())
			})

			Context("When the memory or disk change", func() {
				Context("when there is sufficient memory and disk for the registered tasks", func() {
					It("should update the registry with the new memory and disk values", func() {
						loadedTaskRegistry, err := LoadTaskRegistryFromDisk(513, 2049)
						Ω(err).ShouldNot(HaveOccurred())
						Ω(loadedTaskRegistry.ExecutorMemoryMB).To(Equal(513))
						Ω(loadedTaskRegistry.ExecutorDiskMB).To(Equal(2049))
					})
				})

				Context("when there is insufficient memory for the registered tasks", func() {
					It("should log and return an error", func() {
						_, err := LoadTaskRegistryFromDisk(255, 1024)
						Ω(err).Should(Equal(ErrorNotEnoughMemoryWhenLoadingSnapshot))
					})
				})

				Context("when there is insufficient disk for the registered tasks", func() {
					It("should log and return an error", func() {
						_, err := LoadTaskRegistryFromDisk(256, 1023)
						Ω(err).Should(Equal(ErrorNotEnoughDiskWhenLoadingSnapshot))
					})
				})
			})
		})

		Context("when the file on disk is invalid", func() {
			BeforeEach(func() {
				ioutil.WriteFile("saved_registry", []byte("ß"), os.ModePerm)
			})

			It("should return an error", func() {
				_, err := LoadTaskRegistryFromDisk(4096, 4096)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("When there is not a task registry on disk", func() {
			It("should return an error", func() {
				_, err := LoadTaskRegistryFromDisk(4096, 4096)
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
