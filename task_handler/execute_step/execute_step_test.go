package execute_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/task_handler/execute_step"
)

var _ = Describe("ExecuteStep", func() {
	var (
		step   sequence.Step
		result chan error

		task       *models.Task
		subStep    sequence.Step
		bbs        *fake_bbs.FakeExecutorBBS
		taskResult *string
	)

	BeforeEach(func() {
		result = make(chan error)

		subStep = nil

		bbs = fake_bbs.NewFakeExecutorBBS()

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

			ExecutorID: "some-executor-id",

			ContainerHandle: "some-container-handle",
		}

		result := "the result of the running"
		taskResult = &result
	})

	JustBeforeEach(func() {
		step = New(
			task,
			steno.NewLogger("test-logger"),
			subStep,
			bbs,
			taskResult,
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

			It("completes the Task in the BBS with Failed false and an empty reason", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				completed := bbs.CompletedTasks()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal(task.Guid))
				Ω(completed[0].Result).Should(Equal(*taskResult))
				Ω(completed[0].Failed).Should(BeFalse())
				Ω(completed[0].FailureReason).Should(BeZero())
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.SetCompleteTaskErr(disaster)
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

			It("completes the Task in the BBS with Failed true and a FailureReason", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				completed := bbs.CompletedTasks()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal(task.Guid))
				Ω(completed[0].Result).Should(Equal(*taskResult))
				Ω(completed[0].Failed).Should(BeTrue())
				Ω(completed[0].FailureReason).Should(Equal("oh no!"))
			})

			Context("when completing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					bbs.SetCompleteTaskErr(disaster)
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

		It("completes the Task with Failed true and a FailureReason", func() {
			go step.Perform()

			step.Cancel()
			Eventually(cancelled).Should(Receive())

			Eventually(bbs.CompletedTasks).ShouldNot(BeEmpty())

			completed := bbs.CompletedTasks()[0]
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
