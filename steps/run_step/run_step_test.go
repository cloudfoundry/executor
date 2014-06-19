package run_step_test

import (
	"bytes"
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/garden/client/connection/fake_connection"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	. "github.com/cloudfoundry-incubator/executor/steps/run_step"
)

var _ = Describe("RunAction", func() {
	var step sequence.Step

	var runAction models.RunAction
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var wardenClient *fake_warden_client.FakeClient
	var logger *steno.Logger
	var fileDescriptorLimit uint64

	var processPayloadStream chan warden.ProcessStream

	BeforeEach(func() {
		fileDescriptorLimit = 17

		runAction = models.RunAction{
			Script: "sudo reboot",
			Env: []models.EnvironmentVariable{
				{Name: "A", Value: "1"},
				{Name: "B", Value: "2"},
			},
			ResourceLimits: models.ResourceLimits{
				Nofile: &fileDescriptorLimit,
			},
		}

		fakeStreamer = new(fake_log_streamer.FakeLogStreamer)

		wardenClient = fake_warden_client.New()

		logger = steno.NewLogger("test-logger")

		stream := make(chan warden.ProcessStream, 1000)
		processPayloadStream = stream

		wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			return 0, stream, nil
		}
	})

	exit0 := uint32(0)
	exit19 := uint32(19)

	successfulExit := warden.ProcessStream{ExitStatus: &exit0}
	failedExit := warden.ProcessStream{ExitStatus: &exit19}

	handle := "some-container-handle"

	JustBeforeEach(func() {
		container, err := wardenClient.Create(warden.ContainerSpec{
			Handle: handle,
		})
		Ω(err).ShouldNot(HaveOccurred())

		step = New(
			container,
			runAction,
			fakeStreamer,
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
				runningScripts := wardenClient.Connection.SpawnedProcesses(handle)
				Ω(runningScripts).Should(HaveLen(1))

				runningScript := runningScripts[0]
				Ω(runningScript.Script).Should(Equal("sudo reboot"))
				Ω(*runningScript.Limits.Nofile).Should(BeNumerically("==", fileDescriptorLimit))
				Ω(runningScript.EnvironmentVariables).Should(Equal([]warden.EnvironmentVariable{
					{"A", "1"},
					{"B", "2"},
				}))
			})
		})

		Context("when a file descriptor limit is not configured", func() {
			BeforeEach(func() {
				runAction.ResourceLimits.Nofile = nil
				processPayloadStream <- successfulExit
			})

			It("does not enforce it on the process", func() {
				runningScripts := wardenClient.Connection.SpawnedProcesses(handle)
				Ω(runningScripts).Should(HaveLen(1))

				runningScript := runningScripts[0]
				Ω(runningScript.Limits.Nofile).Should(BeNil())
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				processPayloadStream <- failedExit
			})

			It("should return an emittable error with the exit code", func() {
				Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Exited with status 19")))
			})
		})

		Context("when Warden errors", func() {
			disaster := errors.New("I, like, tried but failed")

			BeforeEach(func() {
				wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
					return 0, nil, disaster
				}
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
				It("returns an emittable error", func() {
					Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Timed out after 100ms")))
				})
			})
		})

		Context("regardless of status code, when an out of memory event has occured", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenGettingInfo = func(string) (warden.ContainerInfo, error) {
					return warden.ContainerInfo{
						Events: []string{"happy land", "out of memory", "another event"},
					}, nil
				}

				processPayloadStream <- failedExit
			})

			It("returns an emittable error", func() {
				Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Exited with status 19 (out of memory)")))
			})
		})

		Describe("emitting logs", func() {
			var (
				stdoutBuffer *bytes.Buffer
				stderrBuffer *bytes.Buffer
			)

			BeforeEach(func() {
				stdoutBuffer = new(bytes.Buffer)
				stderrBuffer = new(bytes.Buffer)

				fakeStreamer.StdoutReturns(stdoutBuffer)
				fakeStreamer.StderrReturns(stderrBuffer)

				processPayloadStream <- warden.ProcessStream{
					Source: warden.ProcessStreamSourceStdout,
					Data:   []byte("hi out"),
				}

				processPayloadStream <- warden.ProcessStream{
					Source: warden.ProcessStreamSourceStderr,
					Data:   []byte("hi err"),
				}

				processPayloadStream <- successfulExit
			})

			It("emits the output chunks as they come in", func() {
				Ω(stdoutBuffer.String()).Should(ContainSubstring("hi out"))
				Ω(stderrBuffer.String()).Should(ContainSubstring("hi err"))
			})

			It("should flush the output when the code exits", func() {
				Ω(fakeStreamer.FlushCallCount()).Should(Equal(1))
			})
		})
	})

	Describe("Cancel", func() {
		JustBeforeEach(func() {
			step.Cancel()
		})

		It("stops the container", func() {
			step.Cancel()
			Ω(wardenClient.Connection.Stopped(handle)).Should(ContainElement(fake_connection.StopSpec{}))
		})
	})
})
