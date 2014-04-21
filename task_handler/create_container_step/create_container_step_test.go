package create_container_step_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/task_handler/create_container_step"
)

var _ = Describe("CreateContainerStep", func() {
	var step sequence.Step

	var task models.Task
	var gordon *fake_gordon.FakeGordon
	var containerHandle string

	BeforeEach(func() {
		gordon = fake_gordon.New()

		task = models.Task{
			Guid:  "totally-unique",
			Stack: "penguin",
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
			&task,
			steno.NewLogger("test-logger"),
			gordon,
			"container-owner-name",
			&containerHandle,
		)
	})

	Describe("Perform", func() {
		disaster := errors.New("oh no!")

		It("creates a container and updates the Task's ContainerHandle", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(gordon.CreatedHandles()).Should(HaveLen(1))
		})

		It("creates a container with the given properties", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			handles := gordon.CreatedHandles()
			Ω(handles).Should(HaveLen(1))

			properties := gordon.CreatedProperties(handles[0])
			Ω(properties["owner"]).Should(Equal("container-owner-name"))
		})

		It("sets the shared containerHandle pointer", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(containerHandle).Should(Equal(gordon.CreatedHandles()[0]))
		})

		Context("when registering fails", func() {
			BeforeEach(func() {
				gordon.CreateError = disaster
			})

			It("sends back the error", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})

	Describe("Cleanup", func() {
		It("destroys the created container", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			step.Cleanup()

			Ω(gordon.DestroyedHandles()).Should(Equal(gordon.CreatedHandles()))
		})
	})
})
