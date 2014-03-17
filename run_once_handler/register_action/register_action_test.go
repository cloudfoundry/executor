package register_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/run_once_handler/register_action"
	"github.com/cloudfoundry-incubator/executor/task_registry/fake_task_registry"
)

var _ = Describe("RegisterAction", func() {
	var action action_runner.Action

	var runOnce models.RunOnce
	var fakeTaskRegistry *fake_task_registry.FakeTaskRegistry

	BeforeEach(func() {
		fakeTaskRegistry = fake_task_registry.New()

		runOnce = models.RunOnce{
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

		action = New(
			runOnce,
			steno.NewLogger("test-logger"),
			fakeTaskRegistry,
		)
	})

	Describe("Perform", func() {
		It("registers the RunOnce", func() {
			err := action.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(fakeTaskRegistry.RegisteredRunOnces).Should(ContainElement(runOnce))
		})

		Context("when registering fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeTaskRegistry.AddRunOnceErr = disaster
			})

			It("sends back the error", func() {
				err := action.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})

	Describe("Cleanup", func() {
		It("unregisters the RunOnce", func() {
			action.Cleanup()
			Ω(fakeTaskRegistry.UnregisteredRunOnces).Should(ContainElement(runOnce))
		})
	})
})
