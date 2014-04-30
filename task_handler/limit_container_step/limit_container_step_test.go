package limit_container_step_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/task_handler/limit_container_step"
)

var _ = Describe("LimitContainerStep", func() {
	var step sequence.Step

	var task models.Task
	var wardenClient *fake_warden_client.FakeClient
	var container warden.Container
	var containerInodeLimit int

	handle := "some-container-handle"

	BeforeEach(func() {
		wardenClient = fake_warden_client.New()

		var err error

		container, err = wardenClient.Create(warden.ContainerSpec{Handle: handle})
		Ω(err).ShouldNot(HaveOccurred())

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
			containerInodeLimit,
			maxCpuShares,
			&container,
		)
	})

	Describe("Perform", func() {
		var err error
		disaster := errors.New("oh no!")

		It("should limit memory", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(wardenClient.Connection.LimitedMemory(handle)[0].LimitInBytes).Should(BeNumerically("==", 1024*1024*1024))
		})

		Context("when limiting memory fails", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenLimitingMemory = func(string, warden.MemoryLimits) (warden.MemoryLimits, error) {
					return warden.MemoryLimits{}, disaster
				}

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

				Ω(wardenClient.Connection.LimitedMemory(handle)).Should(BeEmpty())
			})
		})

		It("should limit disk", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(wardenClient.Connection.LimitedDisk(handle)[0].ByteLimit).Should(BeNumerically("==", 512*1024*1024))
			Ω(wardenClient.Connection.LimitedDisk(handle)[0].InodeLimit).Should(BeNumerically("==", containerInodeLimit))
		})

		Context("when limiting disk fails", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenLimitingDisk = func(string, warden.DiskLimits) (warden.DiskLimits, error) {
					return warden.DiskLimits{}, disaster
				}

				err = step.Perform()
			})

			It("sends back the error", func() {
				Ω(err).Should(Equal(disaster))
			})
		})

		It("limits the cpu shares for the container", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(wardenClient.Connection.LimitedCPU(handle)[0].LimitInShares).Should(BeNumerically("==", 100))
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

				Ω(wardenClient.Connection.LimitedCPU(handle)).Should(BeEmpty())
			})
		})

		Context("when limiting cpu shares fails", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenLimitingCPU = func(string, warden.CPULimits) (warden.CPULimits, error) {
					return warden.CPULimits{}, disaster
				}

				err = step.Perform()
			})

			It("sends back the error", func() {
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
