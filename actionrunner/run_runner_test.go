package actionrunner_test

import (
	"errors"
	"os"
	"time"

	"code.google.com/p/gogoprotobuf/proto"
	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/emitter/fakeemitter"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader/fakeuploader"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"
)

var _ = Describe("RunRunner", func() {
	var (
		actions     []models.ExecutorAction
		runner      *ActionRunner
		downloader  *fakedownloader.FakeDownloader
		uploader    *fakeuploader.FakeUploader
		gordon      *fake_gordon.FakeGordon
		linuxPlugin *linuxplugin.LinuxPlugin
		emitter     *fakeemitter.FakeEmitter
	)

	var stream chan *warden.ProcessPayload

	BeforeEach(func() {
		gordon = fake_gordon.New()
		downloader = &fakedownloader.FakeDownloader{}
		uploader = &fakeuploader.FakeUploader{}
		linuxPlugin = linuxplugin.New()
		runner = New(gordon, linuxPlugin, downloader, uploader, os.TempDir(), steno.NewLogger("test-logger"))

		actions = []models.ExecutorAction{
			{
				models.RunAction{
					Script: "sudo reboot",
					Env: map[string]string{
						"A": "1",
					},
				},
			},
		}

		stream = make(chan *warden.ProcessPayload, 1000)
		gordon.SetRunReturnValues(0, stream, nil)
	})

	Context("when the script succeeds", func() {
		BeforeEach(func() {
			stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
		})

		It("executes the command in the passed-in container", func() {
			err := runner.Run("handle-x", emitter, actions)
			Ω(err).ShouldNot(HaveOccurred())

			runningScript := gordon.ScriptsThatRan()[0]
			Ω(runningScript.Handle).Should(Equal("handle-x"))
			Ω(runningScript.Script).Should(Equal("export A=\"1\"\nsudo reboot"))
		})
	})

	Context("when gordon errors", func() {
		BeforeEach(func() {
			gordon.SetRunReturnValues(0, nil, errors.New("I, like, tried but failed"))
		})

		It("should return the error", func() {
			err := runner.Run("handle-x", emitter, actions)
			Ω(err).Should(Equal(errors.New("I, like, tried but failed")))
		})
	})

	Context("when the script has a non-zero exit code", func() {
		BeforeEach(func() {
			stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(19)}
		})

		It("should return an error with the exit code", func() {
			err := runner.Run("handle-x", emitter, actions)
			Ω(err.Error()).Should(ContainSubstring("19"))
		})
	})

	Context("when the action does not have a timeout", func() {
		It("does not enforce one (i.e. zero-value time.Duration)", func() {
			go func() {
				time.Sleep(100 * time.Millisecond)
				stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
			}()

			err := runner.Run("handle-x", emitter, actions)
			Ω(err).ShouldNot(HaveOccurred())
		})
	})

	Context("when the action has a timeout", func() {
		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.RunAction{
						Script:  "sudo reboot",
						Timeout: 100 * time.Millisecond,
					},
				},
			}
		})

		Context("and the script completes in time", func() {
			It("succeeds", func() {
				stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}

				err := runner.Run("handle-x", emitter, actions)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("and the script takes longer than the timeout", func() {
			It("returns a RunActionTimeoutError", func() {
				go func() {
					time.Sleep(1 * time.Second)
					stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
				}()

				err := runner.Run("handle-x", emitter, actions)
				Ω(err).Should(HaveOccurred())
				Ω(err).Should(Equal(RunActionTimeoutError{models.RunAction{
					Script:  "sudo reboot",
					Timeout: 100 * time.Millisecond,
				}}))
			})
		})
	})

	Context("when given an emitter", func() {
		stdout := warden.ProcessPayload_stdout
		stderr := warden.ProcessPayload_stderr

		BeforeEach(func() {
			emitter = fakeemitter.New()

			stream <- &warden.ProcessPayload{
				Source: &stdout,
				Data:   proto.String("hi out"),
			}

			stream <- &warden.ProcessPayload{
				Source: &stderr,
				Data:   proto.String("hi err"),
			}

			stream <- &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}
		})

		It("emits the output chunks as they come in", func() {
			err := runner.Run("handle-x", emitter, actions)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(emitter.EmittedStdout).Should(ContainElement("hi out"))
			Ω(emitter.EmittedStderr).Should(ContainElement("hi err"))
		})
	})
})
