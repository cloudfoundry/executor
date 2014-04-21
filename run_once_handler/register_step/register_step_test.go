package register_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/run_once_handler/register_step"
	"github.com/cloudfoundry-incubator/executor/task_registry/fake_task_registry"
)

var _ = Describe("RegisterStep", func() {
	var step sequence.Step

	var task *models.Task
	var fakeTaskRegistry *fake_task_registry.FakeTaskRegistry

	BeforeEach(func() {
		fakeTaskRegistry = fake_task_registry.New()

		task = &models.Task{
			Guid:  "totally-unique",
			Stack: "penguin",
			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
					},
				},
			},
		}

		step = New(
			task,
			steno.NewLogger("test-logger"),
			fakeTaskRegistry,
		)
	})

	Describe("Perform", func() {
		It("registers the Task", func() {
			originalTask := task

			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(fakeTaskRegistry.RegisteredTasks).Should(ContainElement(originalTask))
		})

		Context("when registering fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeTaskRegistry.AddTaskErr = disaster
			})

			It("sends back the error", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})

	Describe("Cleanup", func() {
		It("unregisters the Task", func() {
			originalTask := task

			step.Cleanup()

			Ω(fakeTaskRegistry.UnregisteredTasks).Should(ContainElement(originalTask))
		})
	})
})
