package steps_test

import (
	"bytes"
	"errors"

	"github.com/pivotal-golang/lager/lagertest"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer/fake_log_streamer"

	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/steps/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("EmitProgressStep", func() {
	var step Step
	var subStep Step
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
		cancelled = false
		fakeStreamer = new(fake_log_streamer.FakeLogStreamer)

		fakeStreamer.StderrReturns(stderrBuffer)
		fakeStreamer.StdoutReturns(stdoutBuffer)

		subStep = &fakes.FakeStep{
			PerformStub: func() error {
				fakeStreamer.Stdout().Write([]byte("RUNNING\n"))
				return errorToReturn
			},
			CancelStub: func() {
				cancelled = true
			},
		}

		logger = lagertest.NewTestLogger("test")
	})

	JustBeforeEach(func() {
		step = NewEmitProgress(subStep, startMessage, successMessage, failureMessage, fakeStreamer, logger)
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
						errorToReturn = NewEmittableError(errors.New("bam!"), "Failed to reticulate")
					})

					It("should print out the emittable error", func() {
						step.Perform()

						Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
						Ω(stderrBuffer.String()).Should(Equal("FAIL: Failed to reticulate\n"))
					})

					It("logs the error", func() {
						step.Perform()

						logs := logger.TestSink.Logs()
						Ω(logs).Should(HaveLen(1))

						Ω(logs[0].Message).Should(ContainSubstring("errored"))
						Ω(logs[0].Data["wrapped-error"]).Should(Equal("bam!"))
						Ω(logs[0].Data["message-emitted"]).Should(Equal("Failed to reticulate"))
					})

					Context("without a wrapped error", func() {
						BeforeEach(func() {
							errorToReturn = NewEmittableError(nil, "Failed to reticulate")
						})

						It("should print out the emittable error", func() {
							step.Perform()

							Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
							Ω(stderrBuffer.String()).Should(Equal("FAIL: Failed to reticulate\n"))
						})

						It("logs the error", func() {
							step.Perform()

							logs := logger.TestSink.Logs()
							Ω(logs).Should(HaveLen(1))

							Ω(logs[0].Message).Should(ContainSubstring("errored"))
							Ω(logs[0].Data["wrapped-error"]).Should(BeEmpty())
							Ω(logs[0].Data["message-emitted"]).Should(Equal("Failed to reticulate"))
						})
					})
				})
			})

			Context("and there is no failure message", func() {
				BeforeEach(func() {
					errorToReturn = NewEmittableError(errors.New("bam!"), "Failed to reticulate")
				})

				It("should not emit the failure message or error, even with an emittable error", func() {
					step.Perform()

					Ω(stdoutBuffer.String()).Should(Equal("RUNNING\n"))
					Ω(stderrBuffer.String()).Should(BeEmpty())
				})
			})
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
