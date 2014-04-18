package emit_progress_step_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/steps/emit_progress_step"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("EmitProgressStep", func() {
	var step sequence.Step
	var subStep sequence.Step
	var cleanedUp bool
	var cancelled bool
	var errorToReturn error
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var startMessage, successMessage, failureMessage string
	var fakeLogger *steno.Logger

	BeforeEach(func() {
		errorToReturn = nil
		startMessage, successMessage, failureMessage = "", "", ""
		cleanedUp, cancelled = false, false
		fakeStreamer = fake_log_streamer.New()

		steno.EnterTestMode(steno.LOG_DEBUG)

		subStep = fake_step.FakeStep{
			WhenPerforming: func() error {
				fakeStreamer.Stdout().Write([]byte("RUNNING\n"))
				return errorToReturn
			},
			WhenCleaningUp: func() {
				cleanedUp = true
			},
			WhenCancelling: func() {
				cancelled = true
			},
		}

		fakeLogger = steno.NewLogger("test-logger")
	})

	JustBeforeEach(func() {
		step = New(subStep, startMessage, successMessage, failureMessage, fakeStreamer, fakeLogger)
	})

	Context("running", func() {
		Context("when there is a start message", func() {
			BeforeEach(func() {
				startMessage = "STARTING"
			})

			It("should emit the start message before performing", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("STARTING\nRUNNING\n"))
			})
		})

		Context("when there is no start or success message", func() {
			It("should not emit the start message (i.e. a newline) before performing", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("RUNNING\n"))
			})
		})

		Context("when the substep succeeds and there is a success message", func() {
			BeforeEach(func() {
				successMessage = "SUCCESS"
			})

			It("should emit the sucess message", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("RUNNING\nSUCCESS\n"))
			})
		})

		Context("when the substep fails", func() {
			BeforeEach(func() {
				errorToReturn = errors.New("bam!")
			})

			It("should pass the error along", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(errorToReturn))
			})

			Context("and there is a failure message", func() {
				BeforeEach(func() {
					failureMessage = "FAIL"
				})

				It("should emit the failure message", func() {
					step.Perform()

					Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("RUNNING\n"))
					Ω(fakeStreamer.StderrBuffer.String()).Should(Equal("FAIL\n"))
				})

				Context("with an emittable error", func() {
					BeforeEach(func() {
						errorToReturn = emittable_error.New(errors.New("bam!"), "Failed to reticulate")
					})

					It("should print out the emittable error", func() {
						step.Perform()

						Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("RUNNING\n"))
						Ω(fakeStreamer.StderrBuffer.String()).Should(Equal("FAIL\nFailed to reticulate\n"))
					})
				})
			})

			Context("and there is no failure message", func() {
				BeforeEach(func() {
					errorToReturn = emittable_error.New(errors.New("bam!"), "Failed to reticulate")
				})

				It("should not emit the failure message or error, even with an emittable error", func() {
					step.Perform()

					Ω(fakeStreamer.StdoutBuffer.String()).Should(Equal("RUNNING\n"))
					Ω(fakeStreamer.StderrBuffer.String()).Should(BeEmpty())
				})
			})
		})
	})

	Context("when told to clean up", func() {
		It("passes the message along", func() {
			Ω(cleanedUp).Should(BeFalse())
			step.Cleanup()
			Ω(cleanedUp).Should(BeTrue())
		})
	})

	Context("when told to cancel", func() {
		It("passes the message along", func() {
			Ω(cancelled).Should(BeFalse())
			step.Cancel()
			Ω(cancelled).Should(BeTrue())
		})
	})
})
