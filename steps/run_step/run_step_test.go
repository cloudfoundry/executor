package run_step_test

import (
	"errors"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
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

	var spawnedProcess *wfakes.FakeProcess
	var runError error

	BeforeEach(func() {
		fileDescriptorLimit = 17

		runAction = models.RunAction{
			Path: "sudo",
			Args: []string{"reboot"},
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

		spawnedProcess = new(wfakes.FakeProcess)
		runError = nil

		wardenClient.Connection.RunStub = func(string, warden.ProcessSpec, warden.ProcessIO) (warden.Process, error) {
			return spawnedProcess, runError
		}
	})

	handle := "some-container-handle"

	JustBeforeEach(func() {
		wardenClient.Connection.CreateReturns(handle, nil)

		container, err := wardenClient.Create(warden.ContainerSpec{})
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
				spawnedProcess.WaitReturns(0, nil)
			})

			It("does not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("executes the command in the passed-in container", func() {
				ranHandle, spec, _ := wardenClient.Connection.RunArgsForCall(0)
				Ω(ranHandle).Should(Equal(handle))
				Ω(spec.Path).Should(Equal("sudo"))
				Ω(spec.Args).Should(Equal([]string{"reboot"}))
				Ω(*spec.Limits.Nofile).Should(BeNumerically("==", fileDescriptorLimit))
				Ω(spec.Env).Should(Equal([]string{"A=1", "B=2"}))
			})
		})

		Context("when a file descriptor limit is not configured", func() {
			BeforeEach(func() {
				runAction.ResourceLimits.Nofile = nil
				spawnedProcess.WaitReturns(0, nil)
			})

			It("does not enforce it on the process", func() {
				_, spec, _ := wardenClient.Connection.RunArgsForCall(0)
				Ω(spec.Limits.Nofile).Should(BeNil())
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				spawnedProcess.WaitReturns(19, nil)
			})

			It("should return an emittable error with the exit code", func() {
				Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Exited with status 19")))
			})
		})

		Context("when Warden errors", func() {
			disaster := errors.New("I, like, tried but failed")

			BeforeEach(func() {
				runError = disaster
			})

			It("returns the error", func() {
				Ω(stepErr).Should(Equal(disaster))
			})
		})

		Context("when the step does not have a timeout", func() {
			BeforeEach(func() {
				spawnedProcess.WaitStub = func() (int, error) {
					time.Sleep(100 * time.Millisecond)
					return 0, nil
				}
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
					spawnedProcess.WaitReturns(0, nil)
				})

				It("succeeds", func() {
					Ω(stepErr).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				BeforeEach(func() {
					spawnedProcess.WaitStub = func() (int, error) {
						time.Sleep(1 * time.Second)
						return 0, nil
					}
				})

				It("returns an emittable error", func() {
					Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Timed out after 100ms")))
				})
			})
		})

		Context("regardless of status code, when an out of memory event has occured", func() {
			BeforeEach(func() {
				wardenClient.Connection.InfoReturns(
					warden.ContainerInfo{
						Events: []string{"happy land", "out of memory", "another event"},
					},
					nil,
				)

				spawnedProcess.WaitReturns(19, nil)
			})

			It("returns an emittable error", func() {
				Ω(stepErr).Should(MatchError(emittable_error.New(nil, "Exited with status 19 (out of memory)")))
			})
		})

		Describe("emitting logs", func() {
			var stdoutBuffer, stderrBuffer *gbytes.Buffer

			BeforeEach(func() {
				stdoutBuffer = gbytes.NewBuffer()
				stderrBuffer = gbytes.NewBuffer()

				fakeStreamer.StdoutReturns(stdoutBuffer)
				fakeStreamer.StderrReturns(stderrBuffer)

				spawnedProcess.WaitStub = func() (int, error) {
					_, _, io := wardenClient.Connection.RunArgsForCall(0)

					_, err := io.Stdout.Write([]byte("hi out"))
					Ω(err).ShouldNot(HaveOccurred())

					_, err = io.Stderr.Write([]byte("hi err"))
					Ω(err).ShouldNot(HaveOccurred())

					return 0, nil
				}
			})

			It("emits the output chunks as they come in", func() {
				Ω(stdoutBuffer).Should(gbytes.Say("hi out"))
				Ω(stderrBuffer).Should(gbytes.Say("hi err"))
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
			Ω(wardenClient.Connection.StopCallCount()).Should(Equal(1))

			stoppedHandle, kill := wardenClient.Connection.StopArgsForCall(0)
			Ω(stoppedHandle).Should(Equal(handle))
			Ω(kill).Should(BeFalse())
		})
	})
})
