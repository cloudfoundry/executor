package run_step_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"

	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/steps/run_step"
)

var _ = Describe("RunAction", func() {
	var step sequence.Step

	var runAction models.RunAction
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var backendPlugin *linux_plugin.LinuxPlugin
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger
	var fileDescriptorLimit int

	var processPayloadStream chan *warden.ProcessPayload

	BeforeEach(func() {
		fileDescriptorLimit = 17
		runAction = models.RunAction{
			Name:   "Staging",
			Script: "sudo reboot",
			Env: [][]string{
				{"A", "1"},
			},
		}

		fakeStreamer = fake_log_streamer.New()

		wardenClient = fake_gordon.New()

		backendPlugin = linux_plugin.New()

		logger = steno.NewLogger("test-logger")

		processPayloadStream = make(chan *warden.ProcessPayload, 1000)

		wardenClient.SetRunReturnValues(0, processPayloadStream, nil)
	})

	successfulExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
	failedExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(19)}

	JustBeforeEach(func() {
		step = New(
			"some-container-handle",
			runAction,
			fileDescriptorLimit,
			fakeStreamer,
			backendPlugin,
			wardenClient,
			logger,
		)
	})

	Describe("Perform", func() {
		var stepErr error

		JustBeforeEach(func() {
			stepErr = step.Perform()
		})

		Context("when the script succeeds", func() {
			BeforeEach(func() {
				processPayloadStream <- successfulExit
			})

			It("does not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("executes the command in the passed-in container", func() {
				runningScript := wardenClient.ScriptsThatRan()[0]
				Ω(runningScript.Handle).Should(Equal("some-container-handle"))
				Ω(runningScript.Script).Should(Equal("export A=\"1\"\nsudo reboot"))
				Ω(runningScript.ResourceLimits.FileDescriptors).Should(BeNumerically("==", fileDescriptorLimit))
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				processPayloadStream <- failedExit
			})

			It("should return an error with the exit code", func() {
				Ω(stepErr).Should(HaveOccurred())
				Ω(stepErr.Error()).Should(ContainSubstring("19"))
			})
		})

		Context("when Warden errors", func() {
			disaster := errors.New("I, like, tried but failed")

			BeforeEach(func() {
				wardenClient.SetRunReturnValues(0, nil, disaster)
			})

			It("returns the error", func() {
				Ω(stepErr).Should(Equal(disaster))
			})
		})

		Context("when the step does not have a timeout", func() {
			BeforeEach(func() {
				go func() {
					time.Sleep(100 * time.Millisecond)
					processPayloadStream <- successfulExit
				}()
			})

			It("does not enforce one (i.e. zero-value time.Duration)", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})
		})

		Context("when the step has a timeout", func() {
			BeforeEach(func() {
				runActionWithTimeout := runAction
				runActionWithTimeout.Timeout = 100 * time.Millisecond
				runAction = runActionWithTimeout
			})

			Context("and the script completes in time", func() {
				BeforeEach(func() {
					processPayloadStream <- successfulExit
				})

				It("succeeds", func() {
					Ω(stepErr).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				BeforeEach(func() {
					go func() {
						time.Sleep(1 * time.Second)
						processPayloadStream <- successfulExit
					}()
				})
				It("returns a TimeoutError", func() {
					Ω(stepErr).Should(Equal(TimeoutError{runAction}))
					Ω(stepErr.Error()).Should(Equal("timed out after 100ms"))
				})
			})
		})

		Context("regardless of status code, when an out of memory event has occured", func() {
			BeforeEach(func() {
				wardenClient.SetInfoResponse(&warden.InfoResponse{
					Events: []string{"happy land", "out of memory", "another event"},
				})

				processPayloadStream <- failedExit
			})

			It("returns a RunActionOOMError", func() {
				Ω(stepErr).Should(Equal(OOMError))
				Ω(stepErr.Error()).Should(Equal("out of memory"))
			})

			It("loggregates a message with status code to STDERR", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Staging exited with status 19 (out of memory)"))
			})
		})

		Describe("emitting logs", func() {
			stdout := warden.ProcessPayload_stdout
			stderr := warden.ProcessPayload_stderr

			BeforeEach(func() {
				processPayloadStream <- &warden.ProcessPayload{
					Source: &stdout,
					Data:   proto.String("hi out"),
				}

				processPayloadStream <- &warden.ProcessPayload{
					Source: &stderr,
					Data:   proto.String("hi err"),
				}

				processPayloadStream <- successfulExit
			})

			It("emits the output chunks as they come in", func() {
				Ω(fakeStreamer.StreamedStdout).Should(ContainSubstring("hi out"))
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("hi err"))
			})

			It("should flush the output when the code exits", func() {
				Ω(fakeStreamer.Flushed).Should(BeTrue())
			})
		})
	})

	Describe("Cancel", func() {
		JustBeforeEach(func() {
			step.Cancel()
		})

		It("stops the container", func() {
			step.Cancel()
			Ω(wardenClient.StoppedHandles()).Should(ContainElement("some-container-handle"))
		})
	})
})
