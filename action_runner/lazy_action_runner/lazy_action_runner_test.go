package lazy_action_runner_test

import (
	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/fake_action"
	. "github.com/cloudfoundry-incubator/executor/action_runner/lazy_action_runner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LazyActionRunner", func() {
	var runner action_runner.Action

	var invokedGenerator bool
	var generatedActions []action_runner.Action

	BeforeEach(func() {
		invokedGenerator = false
		generatedActions = nil
	})

	JustBeforeEach(func() {
		runner = New(func() []action_runner.Action {
			invokedGenerator = true
			return generatedActions
		})
	})

	Describe("Perform", func() {
		var performed bool

		BeforeEach(func() {
			performed = false

			generatedActions = []action_runner.Action{
				fake_action.FakeAction{
					WhenPerforming: func() error {
						performed = true
						return nil
					},
				},
			}
		})

		It("invokes the generator and performs its actions", func() {
			err := runner.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(invokedGenerator).Should(BeTrue())
			Ω(performed).Should(BeTrue())
		})
	})

	Describe("Cancel", func() {
		var performing chan bool

		var cancelled bool

		BeforeEach(func() {
			cancelled = false

			canceling := make(chan bool)
			performing = make(chan bool)

			generatedActions = []action_runner.Action{
				fake_action.FakeAction{
					WhenPerforming: func() error {
						performing <- true
						<-canceling
						return nil
					},
					WhenCancelling: func() {
						canceling <- true
						cancelled = true
					},
				},
			}
		})

		Context("when the action is running", func() {
			It("cancels it", func() {
				go runner.Perform()

				Eventually(performing).Should(Receive())

				Ω(cancelled).Should(BeFalse())

				runner.Cancel()

				Ω(cancelled).Should(BeTrue())
			})
		})

		Context("when the action is not yet running", func() {
			It("prevents it from running", func() {
				runner.Cancel()

				err := runner.Perform()
				Ω(err).Should(Equal(action_runner.CancelledError))
			})
		})
	})

	Describe("Cleanup", func() {
		var cleanedUp bool

		BeforeEach(func() {
			cleanedUp = false

			generatedActions = []action_runner.Action{
				fake_action.FakeAction{
					WhenCleaningUp: func() {
						cleanedUp = true
					},
				},
			}
		})

		Context("when the action has performed", func() {
			It("cleans it up", func() {
				err := runner.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				runner.Cleanup()

				Ω(cleanedUp).Should(BeTrue())
			})
		})

		Context("when the action has not yet performed", func() {
			It("does nothing, successfully", func() {
				runner.Cleanup()
			})
		})
	})
})
