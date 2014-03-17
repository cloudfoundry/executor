package run_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"
	"time"

	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"

	. "github.com/cloudfoundry-incubator/executor/actions/run_action"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
)

var _ = Describe("RunAction", func() {
	var action action_runner.Action

	var runAction models.RunAction
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var streamer log_streamer.LogStreamer
	var backendPlugin *linux_plugin.LinuxPlugin
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger
	var fileDescriptorLimit int

	var processPayloadStream chan *warden.ProcessPayload

	BeforeEach(func() {
		fileDescriptorLimit = 17
		runAction = models.RunAction{
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
		action = New(
			"some-container-handle",
			runAction,
			fileDescriptorLimit,
			streamer,
			backendPlugin,
			wardenClient,
			logger,
		)
	})

	Describe("Perform", func() {
		Context("when the script succeeds", func() {
			BeforeEach(func() {
				processPayloadStream <- successfulExit
			})

			It("executes the command in the passed-in container", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

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
				err := action.Perform()
				if Ω(err).Should(HaveOccurred()) {
					Ω(err.Error()).Should(ContainSubstring("19"))
				}
			})
		})

		Context("when the action does not have a timeout", func() {
			It("does not enforce one (i.e. zero-value time.Duration)", func() {
				go func() {
					time.Sleep(100 * time.Millisecond)
					processPayloadStream <- successfulExit
				}()

				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the action has a timeout", func() {
			BeforeEach(func() {
				runActionWithTimeout := runAction
				runActionWithTimeout.Timeout = 100 * time.Millisecond

				runAction = runActionWithTimeout
			})

			Context("and the script completes in time", func() {
				It("succeeds", func() {
					processPayloadStream <- successfulExit

					err := action.Perform()
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				It("returns a TimeoutError", func() {
					go func() {
						time.Sleep(1 * time.Second)
						processPayloadStream <- successfulExit
					}()

					err := action.Perform()
					Ω(err).Should(Equal(TimeoutError{runAction}))
					Ω(err.Error()).Should(Equal("timed out after 100ms"))
				})
			})
		})

		Context("regardless of status code, when an out of memory event has occured", func() {
			BeforeEach(func() {
				wardenClient.SetInfoResponse(&warden.InfoResponse{
					Events: []string{"happy land", "out of memory", "another event"},
				})

				processPayloadStream <- successfulExit
			})

			It("returns a RunActionOOMError", func() {
				err := action.Perform()
				Ω(err).Should(Equal(OOMError))
				Ω(err.Error()).Should(Equal("out of memory"))
			})
		})

		Context("when given an emitter", func() {
			stdout := warden.ProcessPayload_stdout
			stderr := warden.ProcessPayload_stderr

			BeforeEach(func() {
				streamer = fakeStreamer

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
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StreamedStdout).Should(ContainElement("hi out"))
				Ω(fakeStreamer.StreamedStderr).Should(ContainElement("hi err"))
			})

			It("should flush the output when the code exits", func() {
				err := action.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.Flushed).Should(BeTrue())
			})
		})

		Context("when Warden errors", func() {
			disaster := errors.New("I, like, tried but failed")

			BeforeEach(func() {
				wardenClient.SetRunReturnValues(0, nil, disaster)
			})

			It("returns the error", func() {
				err := action.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})

	Describe("Cancel", func() {
		It("stops the container", func() {
			action.Cancel()
			Ω(wardenClient.StoppedHandles()).Should(ContainElement("some-container-handle"))
		})
	})
})
