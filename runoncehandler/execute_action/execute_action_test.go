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

		runOnce   models.RunOnce
		subAction action_runner.Action
	)

	BeforeEach(func() {
		result = make(chan error)

		subAction = nil

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
			subAction,
		)
	})

	Describe("Perform", func() {
		Context("when the sub-action succeeds", func() {
			BeforeEach(func() {
				subAction = fake_action.FakeAction{
					WhenPerforming: func() error {
						return nil
					},
				}
			})

			It("returns no error and has Failed as false", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.Failed).Should(BeFalse())
			})
		})

		Context("when the sub-action fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				subAction = fake_action.FakeAction{
					WhenPerforming: func() error {
						return disaster
					},
				}
			})

			It("returns no error and has Failed as true with a reason", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(runOnce.Failed).Should(BeTrue())
				Ω(runOnce.FailureReason).Should(Equal("oh no!"))
			})
		})
	})

	Describe("Cancel", func() {
		var cancelled chan bool

		BeforeEach(func() {
			cancel := make(chan bool)

			cancelled = make(chan bool)

			subAction = fake_action.FakeAction{
				WhenPerforming: func() error {
					<-cancel
					cancelled <- true
					return nil
				},
				WhenCancelling: func() {
					cancel <- true
				},
			}
		})

		It("cancels its action", func() {
			go action.Perform()

			action.Cancel()
			Eventually(cancelled).Should(Receive())
		})
	})

	Describe("Cleanup", func() {
		var cleanedUp chan bool

		BeforeEach(func() {
			cleanUp := make(chan bool)

			cleanedUp = make(chan bool)

			subAction = fake_action.FakeAction{
				WhenPerforming: func() error {
					<-cleanUp
					cleanedUp <- true
					return nil
				},
				WhenCleaningUp: func() {
					cleanUp <- true
				},
			}
		})

		It("cancels its action", func() {
			go action.Perform()

			action.Cleanup()
			Eventually(cleanedUp).Should(Receive())
		})
	})
})
