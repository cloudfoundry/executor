package limit_container_step_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/task_handler/limit_container_step"
)

var _ = Describe("LimitContainerStep", func() {
	var step sequence.Step

	var task models.Task
	var gordon *fake_gordon.FakeGordon
	var handle string
	var containerInodeLimit int

	BeforeEach(func() {
		gordon = fake_gordon.New()
		handle = "some-container-handle"
		containerInodeLimit = 200000
		task = models.Task{
			Guid:       "totally-unique",
			Stack:      "penguin",
			MemoryMB:   1024,
			DiskMB:     512,
			CpuPercent: 25,
			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
					},
				},
			},

			ExecutorID: "some-executor-id",
		}

		maxCpuShares := 400

		step = New(
			&task,
			steno.NewLogger("test-logger"),
			gordon,
			containerInodeLimit,
			maxCpuShares,
			&handle,
		)
	})

	Describe("Perform", func() {
		var err error
		disaster := errors.New("oh no!")

		It("should limit memory", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(gordon.MemoryLimits()).Should(HaveLen(1))
			Ω(gordon.MemoryLimits()[0].Handle).Should(Equal(handle))
			Ω(gordon.MemoryLimits()[0].Limit).Should(BeNumerically("==", 1024*1024*1024))
		})

		Context("when limiting memory fails", func() {
			BeforeEach(func() {
				gordon.SetLimitMemoryError(disaster)
				err = step.Perform()
			})

			It("sends back the error", func() {
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when the memory limit is set to 0", func() {
			BeforeEach(func() {
				task.MemoryMB = 0
			})

			It("should not limit memory", func() {
				err := step.Perform()
				Ω(err).Should(BeNil())

				Ω(gordon.MemoryLimits()).Should(BeEmpty())
			})
		})

		It("should limit disk", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(gordon.DiskLimits()).Should(HaveLen(1))
			Ω(gordon.DiskLimits()[0].Handle).Should(Equal(handle))
			Ω(gordon.DiskLimits()[0].Limits.ByteLimit).Should(BeNumerically("==", 512*1024*1024))
			Ω(gordon.DiskLimits()[0].Limits.InodeLimit).Should(BeNumerically("==", containerInodeLimit))
		})

		Context("when limiting disk fails", func() {
			BeforeEach(func() {
				gordon.SetLimitDiskError(disaster)
				err = step.Perform()
			})

			It("sends back the error", func() {
				Ω(err).Should(Equal(disaster))
			})
		})

		It("limits the cpu shares for the container", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(gordon.CPULimits()).Should(HaveLen(1))
			Ω(gordon.CPULimits()[0].Handle).Should(Equal(handle))
			Ω(gordon.CPULimits()[0].Limit).Should(BeNumerically("==", 100))
		})

		It("returns an error unless the cpu percentage is between 0 and 100", func() {
			task.CpuPercent = 101.0
			err := step.Perform()
			Ω(err).Should(Equal(ErrPercentOutOfBounds))

			task.CpuPercent = 100.0
			err = step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			task.CpuPercent = 1.0
			err = step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			task.CpuPercent = -1.0
			err = step.Perform()
			Ω(err).Should(Equal(ErrPercentOutOfBounds))
		})

		Context("when no cpu percentage is specified", func() {
			BeforeEach(func() {
				task.CpuPercent = 0.0
			})

			It("does not limit the cpu", func() {
				err := step.Perform()
				Ω(err).Should(BeNil())
				Ω(gordon.CPULimits()).Should(BeEmpty())
			})
		})

		Context("when limiting cpu shares fails", func() {
			BeforeEach(func() {
				gordon.SetLimitCPUError(disaster)
				err = step.Perform()
			})

			It("sends back the error", func() {
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
