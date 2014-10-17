package emit_progress_step_test

import (
	"bytes"
	"errors"

	"github.com/cloudfoundry-incubator/executor/depot/steps/emittable_error"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/sequence/fake_step"
	. "github.com/cloudfoundry-incubator/executor/depot/steps/emit_progress_step"

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
	var logger *lagertest.TestLogger
	var stderrBuffer *bytes.Buffer
	var stdoutBuffer *bytes.Buffer

	BeforeEach(func() {
		stderrBuffer = new(bytes.Buffer)
		stdoutBuffer = new(bytes.Buffer)
		errorToReturn = nil
		startMessage, successMessage, failureMessage = "", "", ""
		cleanedUp, cancelled = false, false
		fakeStreamer = new(fake_log_streamer.FakeLogStreamer)

		fakeStreamer.StderrReturns(stderrBuffer)
		fakeStreamer.StdoutReturns(stdoutBuffer)

		subStep = &fake_step.FakeStep{
			PerformStub: func() error {
				fakeStreamer.Stdout().Write([]byte("RUNNING\n"))
				return errorToReturn
			},
			CleanupStub: func() {
				cleanedUp = true
			},
			CancelStub: func() {
				cancelled = true
			},
		}

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		step = New(subStep, startMessage, successMessage, failureMessage, fakeStreamer, logger)
	})

	Context("running", func() {
		Context("when there is a start message", func() {
			BeforeEach(func() {
				startMessage = "STARTING"
			})

			It("should emit the start message before performing", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(stdoutBuffer.String()).Should(Equal("STARTING\nRUNNING\n"))
			})
		})

		Context("when there is no start or success message", func() {
			It("should not emit the start message (i.e. a newline) before performing", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
			})
		})

		Context("when the substep succeeds and there is a success message", func() {
			BeforeEach(func() {
				successMessage = "SUCCESS"
			})

			It("should emit the sucess message", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(stdoutBuffer.String()).Should(Equal("RUNNING\nSUCCESS\n"))
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

					Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
					Ω(stderrBuffer.String()).Should(Equal("FAIL\n"))
				})

				Context("with an emittable error", func() {
					BeforeEach(func() {
						errorToReturn = emittable_error.New(errors.New("bam!"), "Failed to reticulate")
					})

					It("should print out the emittable error", func() {
						step.Perform()

						Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
						Ω(stderrBuffer.String()).Should(Equal("FAIL\nFailed to reticulate\n"))
					})
				})
			})

			Context("and there is no failure message", func() {
				BeforeEach(func() {
					errorToReturn = emittable_error.New(errors.New("bam!"), "Failed to reticulate")
				})

				It("should not emit the failure message or error, even with an emittable error", func() {
					step.Perform()

					Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
					Ω(stderrBuffer.String()).Should(BeEmpty())
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
