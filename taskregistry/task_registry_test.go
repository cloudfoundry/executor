package taskregistry_test

import (
	"fmt"
	"github.com/onsi/ginkgo/config"
	"io/ioutil"
	"os"

	. "github.com/cloudfoundry-incubator/executor/taskregistry"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskRegistry", func() {
	var taskRegistry *TaskRegistry
	var runOnce models.RunOnce
	var registryFileName string

	BeforeEach(func() {
		registryFileName = fmt.Sprintf("/tmp/executor_registry_%d", config.GinkgoConfig.ParallelNode)

		runOnce = models.RunOnce{
			MemoryMB: 255,
			DiskMB:   1023,
			Guid:     "a guid",
			Stack:    "some-stack",
		}

		taskRegistry = NewTaskRegistry("some-stack", registryFileName, 256, 1024)
	})

	AfterEach(func() {
		os.Remove(registryFileName)
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
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when not configured with a stack", func() {
			BeforeEach(func() {
				runOnceWithNoStack := runOnce
				runOnceWithNoStack.Stack = ""

				runOnce = runOnceWithNoStack
			})

			It("accepts RunOnces without discrimination", func() {
				err := taskRegistry.AddRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(taskRegistry.RunOnces[runOnce.Guid]).To(Equal(runOnce))
			})
		})

		Context("when there aren't enough resources", func() {
			BeforeEach(func() {
				err := taskRegistry.AddRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})

			It("returns an error and adds nothing when the new RunOnce needs more memory than is available", func() {
				err := taskRegistry.AddRunOnce(models.RunOnce{
					MemoryMB: 2,
				})
				Ω(err).Should(HaveOccurred())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
			})

			It("returns an error and adds nothing when the new RunOnce needs more disk than is available", func() {
				err := taskRegistry.AddRunOnce(models.RunOnce{
					DiskMB: 2,
				})
				Ω(err).Should(HaveOccurred())
				Ω(taskRegistry.RunOnces).To(HaveLen(1))
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

	Describe("WriteToDisk", func() {
		It("returns an error if the file cannot be written to", func() {
			taskRegistry = NewTaskRegistry("some-stack", "/tmp", 256, 1024)
			Ω(taskRegistry.WriteToDisk()).To(HaveOccurred())
		})
	})

	Describe("LoadTaskRegistryFromDisk", func() {
		var diskRegistry *TaskRegistry

		Context("when there is a valid task registry on disk", func() {
			BeforeEach(func() {
				diskRegistry = NewTaskRegistry("some-stack", registryFileName, 512, 2048)

				runOnceWithDifferentUsage := runOnce
				runOnceWithDifferentUsage.MemoryMB = 256
				runOnceWithDifferentUsage.DiskMB = 1024

				runOnce = runOnceWithDifferentUsage

				err := diskRegistry.AddRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = diskRegistry.WriteToDisk()
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("loads up the task registry and returns it", func() {
				loadedTaskRegistry, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 512, 2048)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(loadedTaskRegistry.RunOnces).To(HaveLen(1))
				Ω(loadedTaskRegistry.RunOnces["a guid"]).To(Equal(runOnce))
			})

			Context("when the memory or disk change", func() {
				Context("when there is sufficient memory and disk for the registered tasks", func() {
					It("updates the registry with the new memory and disk values", func() {
						loadedTaskRegistry, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 513, 2049)
						Ω(err).ShouldNot(HaveOccurred())
						Ω(loadedTaskRegistry.ExecutorMemoryMB).To(Equal(513))
						Ω(loadedTaskRegistry.ExecutorDiskMB).To(Equal(2049))
					})
				})

				Context("when there is insufficient memory for the registered tasks", func() {
					It("logs and returns an error", func() {
						_, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 255, 1024)
						Ω(err).Should(Equal(ErrorNotEnoughMemoryWhenLoadingSnapshot))
					})
				})

				Context("when there is insufficient disk for the registered tasks", func() {
					It("logs and returns an error", func() {
						_, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 256, 1023)
						Ω(err).Should(Equal(ErrorNotEnoughDiskWhenLoadingSnapshot))
					})
				})
			})
		})

		Context("when the file on disk is invalid", func() {
			BeforeEach(func() {
				ioutil.WriteFile(registryFileName, []byte("ß"), os.ModePerm)
			})

			It("returns an error", func() {
				_, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 4096, 4096)
				Ω(err).Should(Equal(ErrorRegistrySnapshotHasInvalidJSON))
			})
		})

		Context("When there is not a task registry on disk", func() {
			It("returns an error", func() {
				_, err := LoadTaskRegistryFromDisk("some-stack", registryFileName, 4096, 4096)
				Ω(err).Should(Equal(ErrorRegistrySnapshotDoesNotExist))
			})
		})
	})
})
