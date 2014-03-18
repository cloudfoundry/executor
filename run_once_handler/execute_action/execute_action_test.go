package execute_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/fake_action"
	. "github.com/cloudfoundry-incubator/executor/run_once_handler/execute_action"
)

var _ = Describe("ExecuteAction", func() {
	var (
		action action_runner.Action
		result chan error

		runOnce       models.RunOnce
		subAction     action_runner.Action
		bbs           *fake_bbs.FakeExecutorBBS
		runOnceResult string
	)

	BeforeEach(func() {
		result = make(chan error)

		subAction = nil

		bbs = fake_bbs.NewFakeExecutorBBS()

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

		runOnceResult = "the result of the running"
	})

	JustBeforeEach(func() {
		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			subAction,
			bbs,
			&runOnceResult,
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

			It("completes the RunOnce in the BBS with Failed false and an empty reason", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(bbs.CompletedRunOnce.Guid).Should(Equal(runOnce.Guid))
				Ω(bbs.CompletedRunOnce.Result).Should(Equal(runOnceResult))
				Ω(bbs.CompletedRunOnce.Failed).Should(BeFalse())
				Ω(bbs.CompletedRunOnce.FailureReason).Should(BeZero())
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.CompleteRunOnceErr = disaster
				})

				It("returns the error", func() {
					err := action.Perform()
					Ω(err).Should(Equal(disaster))
				})
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

			It("completes the RunOnce in the BBS with Failed true and a FailureReason", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(bbs.CompletedRunOnce.Guid).Should(Equal(runOnce.Guid))
				Ω(bbs.CompletedRunOnce.Result).Should(Equal(runOnceResult))
				Ω(bbs.CompletedRunOnce.Failed).Should(BeTrue())
				Ω(bbs.CompletedRunOnce.FailureReason).Should(Equal("oh no!"))
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.CompleteRunOnceErr = disaster
				})

				It("returns the error", func() {
					err := action.Perform()
					Ω(err).Should(Equal(disaster))
				})
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
					return action_runner.CancelledError
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

		It("completes the RunOnce with Failed true and a FailureReason", func() {
			go action.Perform()

			action.Cancel()
			Eventually(cancelled).Should(Receive())

			Eventually(func() interface{} {
				if bbs.CompletedRunOnce == nil {
					return false
				}
				return bbs.CompletedRunOnce.Failed
			}).Should(BeTrue())

			Ω(bbs.CompletedRunOnce.FailureReason).Should(ContainSubstring("cancelled"))
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

		It("cleans up its action", func() {
			go action.Perform()

			action.Cleanup()
			Eventually(cleanedUp).Should(Receive())
		})
	})
})
