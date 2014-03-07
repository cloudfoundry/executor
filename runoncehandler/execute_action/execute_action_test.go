package execute_action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/fake_action"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action"
)

var _ = Describe("ExecuteAction", func() {
	var (
		action action_runner.Action
		result chan error

		runOnce      models.RunOnce
		actionRunner *action_runner.ActionRunner
	)

	BeforeEach(func() {
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

			ExecutorID: "some-executor-id",

			ContainerHandle: "some-container-handle",
		}
	})

	JustBeforeEach(func() {
		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			actionRunner,
		)
	})

	Describe("Perform", func() {
		Context("when the sub-actions succeed", func() {
			BeforeEach(func() {
				actionRunner = action_runner.New([]action_runner.Action{
					fake_action.FakeAction{
						WhenPerforming: func(result chan<- error) {
							result <- nil
						},
					},
				})
			})

			It("sends back no error and has Failed as false", func() {
				go action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())

				Ω(runOnce.Failed).Should(BeFalse())
			})
		})

		Context("when the sub-actions fail", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				actionRunner = action_runner.New([]action_runner.Action{
					fake_action.FakeAction{
						WhenPerforming: func(result chan<- error) {
							result <- disaster
						},
					},
				})
			})

			It("sends back no error and has Failed as false", func() {
				go action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())

				Ω(runOnce.Failed).Should(BeTrue())
				Ω(runOnce.FailureReason).Should(Equal("oh no!"))
			})
		})
	})
})
