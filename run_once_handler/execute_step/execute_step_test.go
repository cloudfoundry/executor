package execute_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/run_once_handler/execute_step"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
)

var _ = Describe("ExecuteStep", func() {
	var (
		step   sequence.Step
		result chan error

		runOnce       *models.RunOnce
		subStep       sequence.Step
		bbs           *fake_bbs.FakeExecutorBBS
		runOnceResult *string
	)

	BeforeEach(func() {
		result = make(chan error)

		subStep = nil

		bbs = fake_bbs.NewFakeExecutorBBS()

		runOnce = &models.RunOnce{
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

		result := "the result of the running"
		runOnceResult = &result
	})

	JustBeforeEach(func() {
		step = New(
			runOnce,
			steno.NewLogger("test-logger"),
			subStep,
			bbs,
			runOnceResult,
		)
	})

	Describe("Perform", func() {
		Context("when the sub-step succeeds", func() {
			BeforeEach(func() {
				subStep = fake_step.FakeStep{
					WhenPerforming: func() error {
						return nil
					},
				}
			})

			It("completes the RunOnce in the BBS with Failed false and an empty reason", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				completed := bbs.CompletedRunOnces()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal(runOnce.Guid))
				Ω(completed[0].Result).Should(Equal(*runOnceResult))
				Ω(completed[0].Failed).Should(BeFalse())
				Ω(completed[0].FailureReason).Should(BeZero())
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.SetCompleteRunOnceErr(disaster)
				})

				It("returns the error", func() {
					err := step.Perform()
					Ω(err).Should(Equal(disaster))
				})
			})
		})

		Context("when the sub-step fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				subStep = fake_step.FakeStep{
					WhenPerforming: func() error {
						return disaster
					},
				}
			})

			It("completes the RunOnce in the BBS with Failed true and a FailureReason", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				completed := bbs.CompletedRunOnces()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal(runOnce.Guid))
				Ω(completed[0].Result).Should(Equal(*runOnceResult))
				Ω(completed[0].Failed).Should(BeTrue())
				Ω(completed[0].FailureReason).Should(Equal("oh no!"))
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.SetCompleteRunOnceErr(disaster)
				})

				It("returns the error", func() {
					err := step.Perform()
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

			subStep = fake_step.FakeStep{
				WhenPerforming: func() error {
					<-cancel
					cancelled <- true
					return sequence.CancelledError
				},
				WhenCancelling: func() {
					cancel <- true
				},
			}
		})

		It("cancels its step", func() {
			go step.Perform()

			step.Cancel()
			Eventually(cancelled).Should(Receive())
		})

		It("completes the RunOnce with Failed true and a FailureReason", func() {
			go step.Perform()

			step.Cancel()
			Eventually(cancelled).Should(Receive())

			Eventually(bbs.CompletedRunOnces).ShouldNot(BeEmpty())

			completed := bbs.CompletedRunOnces()[0]
			Ω(completed.Failed).Should(BeTrue())
			Ω(completed.FailureReason).Should(ContainSubstring("cancelled"))
		})
	})

	Describe("Cleanup", func() {
		var cleanedUp chan bool

		BeforeEach(func() {
			cleanUp := make(chan bool)

			cleanedUp = make(chan bool)

			subStep = fake_step.FakeStep{
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

		It("cleans up its step", func() {
			go step.Perform()

			step.Cleanup()
			Eventually(cleanedUp).Should(Receive())
		})
	})
})
