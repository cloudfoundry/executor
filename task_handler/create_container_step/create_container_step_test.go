package create_container_step_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/task_handler/create_container_step"
)

var _ = Describe("CreateContainerStep", func() {
	var step sequence.Step

	var task models.Task
	var wardenClient *fake_warden_client.FakeClient
	var container warden.Container

	BeforeEach(func() {
		wardenClient = fake_warden_client.New()

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
			wardenClient,
			"container-owner-name",
			&container,
		)
	})

	Describe("Perform", func() {
		disaster := errors.New("oh no!")

		It("creates a container and updates the Task's ContainerHandle", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(wardenClient.Connection.Created()).Should(HaveLen(1))
		})

		It("creates a container with the given properties", func() {
			err := step.Perform()
			Ω(err).Should(BeNil())

			created := wardenClient.Connection.Created()
			Ω(created).Should(HaveLen(1))

			properties := created[0].Properties
			Ω(properties["owner"]).Should(Equal("container-owner-name"))
		})

		It("sets the shared container pointer", func() {
			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}

			err := step.Perform()
			Ω(err).Should(BeNil())

			Ω(container.Handle()).Should(Equal("some-handle"))
		})

		Context("when registering fails", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
					return "", disaster
				}
			})

			It("sends back the error", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})

	Describe("Cleanup", func() {
		It("destroys the created container", func() {
			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}

			err := step.Perform()
			Ω(err).Should(BeNil())

			step.Cleanup()

			Ω(wardenClient.Connection.Destroyed()).Should(ContainElement("some-handle"))
		})
	})
})
