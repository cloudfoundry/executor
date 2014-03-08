package run_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"
	"time"

	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/logstreamer"
	"github.com/cloudfoundry-incubator/executor/logstreamer/fakelogstreamer"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/run_action"
)

var _ = Describe("RunAction", func() {
	var action action_runner.Action
	var result chan error

	var runOnce *models.RunOnce
	var runAction models.RunAction
	var fakeStreamer *fakelogstreamer.FakeLogStreamer
	var streamer logstreamer.LogStreamer
	var backendPlugin *linuxplugin.LinuxPlugin
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger

	var processPayloadStream chan *warden.ProcessPayload

	BeforeEach(func() {
		result = make(chan error)

		runOnce = &models.RunOnce{
			ContainerHandle: "some-container-handle",
		}

		runAction = models.RunAction{
			Script: "sudo reboot",
			Env: [][]string{
				{"A", "1"},
			},
		}

		fakeStreamer = fakelogstreamer.New()

		wardenClient = fake_gordon.New()

		backendPlugin = linuxplugin.New()

		logger = steno.NewLogger("test-logger")

		processPayloadStream = make(chan *warden.ProcessPayload, 1000)

		wardenClient.SetRunReturnValues(0, processPayloadStream, nil)
	})

	successfulExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
	failedExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(19)}

	JustBeforeEach(func() {
		action = New(
			runOnce,
			runAction,
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
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())

				runningScript := wardenClient.ScriptsThatRan()[0]
				Ω(runningScript.Handle).Should(Equal("some-container-handle"))
				Ω(runningScript.Script).Should(Equal("export A=\"1\"\nsudo reboot"))
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				processPayloadStream <- failedExit
			})

			It("should return an error with the exit code", func() {
				result := make(chan error, 1)
				action.Perform(result)

				err := <-result
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

				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())
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

					result := make(chan error, 1)
					action.Perform(result)
					Ω(<-result).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				It("returns a RunActionTimeoutError", func() {
					go func() {
						time.Sleep(1 * time.Second)
						processPayloadStream <- successfulExit
					}()

					result := make(chan error, 1)
					action.Perform(result)
					Ω(<-result).Should(Equal(RunActionTimeoutError{runAction}))
				})
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
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StreamedStdout).Should(ContainElement("hi out"))
				Ω(fakeStreamer.StreamedStderr).Should(ContainElement("hi err"))
			})

			It("should flush the output when the code exits", func() {
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.Flushed).Should(BeTrue())
			})
		})

		Context("when Warden errors", func() {
			disaster := errors.New("I, like, tried but failed")

			BeforeEach(func() {
				wardenClient.SetRunReturnValues(0, nil, disaster)
			})

			It("sends back the error", func() {
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
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
