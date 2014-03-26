package limit_container_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	. "github.com/cloudfoundry-incubator/executor/run_once_handler/limit_container_step"
)

var _ = Describe("LimitContainerStep", func() {
	var step sequence.Step

	var runOnce models.RunOnce
	var gordon *fake_gordon.FakeGordon
	var handle string
	var containerInodeLimit int

	BeforeEach(func() {
		gordon = fake_gordon.New()
		handle = "some-container-handle"
		containerInodeLimit = 200000
		runOnce = models.RunOnce{
			Guid:     "totally-unique",
			Stack:    "penguin",
			MemoryMB: 1024,
			DiskMB:   512,
			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
					},
				},
			},

			ExecutorID: "some-executor-id",
		}

		step = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			gordon,
			containerInodeLimit,
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
	})
})
