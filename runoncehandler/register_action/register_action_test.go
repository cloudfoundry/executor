package register_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/runoncehandler/register_action"
	"github.com/cloudfoundry-incubator/executor/taskregistry/faketaskregistry"
)

var _ = Describe("RegisterAction", func() {
	var action action_runner.Action
	var result chan error

	var runOnce models.RunOnce
	var fakeTaskRegistry *faketaskregistry.FakeTaskRegistry

	BeforeEach(func() {
		fakeTaskRegistry = faketaskregistry.New()

		result = make(chan error)

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
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(fakeTaskRegistry.RegisteredRunOnces).Should(ContainElement(runOnce))
		})

		Context("when registering fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeTaskRegistry.AddRunOnceErr = disaster
			})

			It("sends back the error", func() {
				go action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
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
