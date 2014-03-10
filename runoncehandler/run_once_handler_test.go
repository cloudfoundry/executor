package runoncehandler_test

import (
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/executor/logstreamer"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler"
	"github.com/cloudfoundry-incubator/executor/taskregistry/faketaskregistry"
	"github.com/cloudfoundry-incubator/executor/uploader/fakeuploader"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
)

var _ = Describe("RunOnceHandler", func() {
	Describe("RunOnce", func() {
		var (
			handler *RunOnceHandler
			runOnce models.RunOnce

			bbs          *fakebbs.FakeExecutorBBS
			wardenClient *fake_gordon.FakeGordon
			downloader   *fakedownloader.FakeDownloader
			uploader     *fakeuploader.FakeUploader
			transformer  *run_once_transformer.RunOnceTransformer
			taskRegistry *faketaskregistry.FakeTaskRegistry
		)

		BeforeEach(func() {
			runOnce = models.RunOnce{
				Guid: "run-once-guid",
				Actions: []models.ExecutorAction{
					{
						Action: models.DownloadAction{
							From: "http://download-src.com",
							To:   "/download-dst",
						},
					},
					{
						Action: models.RunAction{
							Script: "sudo reboot",
						},
					},
					{
						Action: models.UploadAction{
							From: "/upload-src",
							To:   "http://upload-dst.com",
						},
					},
				},
			}

			bbs = fakebbs.NewFakeExecutorBBS()
			wardenClient = fake_gordon.New()
			taskRegistry = faketaskregistry.New()

			logStreamerFactory := func(models.LogConfig) logstreamer.LogStreamer {
				return nil
			}

			logger := steno.NewLogger("test-logger")

			backendPlugin := linuxplugin.New()
			downloader = &fakedownloader.FakeDownloader{}
			uploader = &fakeuploader.FakeUploader{}

			tmpDir, err := ioutil.TempDir("", "run-once-handler-tmp")
			Ω(err).ShouldNot(HaveOccurred())

			transformer = run_once_transformer.NewRunOnceTransformer(
				logStreamerFactory,
				downloader,
				uploader,
				backendPlugin,
				wardenClient,
				logger,
				tmpDir,
			)

			handler = New(
				bbs,
				wardenClient,
				taskRegistry,
				transformer,
				logStreamerFactory,
				logger,
			)
		})

		setupSuccessfulRuns := func() {
			processPayloadStream := make(chan *warden.ProcessPayload, 1000)

			wardenClient.SetRunReturnValues(0, processPayloadStream, nil)

			successfulExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}

			go func() {
				processPayloadStream <- successfulExit
			}()
		}

		BeforeEach(setupSuccessfulRuns)

		It("registers, claims, creates container, starts, (executes...), completes", func() {
			handler.RunOnce(runOnce, "fake-executor-id")

			// register
			Ω(taskRegistry.RegisteredRunOnces).Should(ContainElement(runOnce))

			// claim
			Ω(bbs.ClaimedRunOnce.Guid).Should(Equal("run-once-guid"))
			Ω(bbs.ClaimedRunOnce.ExecutorID).Should(Equal("fake-executor-id"))

			// create container
			Ω(wardenClient.CreatedHandles()).ShouldNot(BeEmpty())

			// start
			Ω(bbs.StartedRunOnce.Guid).Should(Equal("run-once-guid"))
			Ω(bbs.StartedRunOnce.ExecutorID).Should(Equal("fake-executor-id"))
			Ω(bbs.StartedRunOnce.ContainerHandle).ShouldNot(BeZero())

			// execute download action
			Ω(downloader.DownloadedUrls).ShouldNot(BeEmpty())
			Ω(downloader.DownloadedUrls[0].String()).Should(Equal("http://download-src.com"))

			// execute run action
			ranScripts := []string{}
			for _, script := range wardenClient.ScriptsThatRan() {
				Ω(script.Handle).Should(Equal(bbs.StartedRunOnce.ContainerHandle))

				ranScripts = append(ranScripts, script.Script)
			}

			Ω(ranScripts).Should(ContainElement("sudo reboot"))

			// execute upload action
			Ω(uploader.UploadUrls).ShouldNot(BeEmpty())
			Ω(uploader.UploadUrls[0].String()).Should(Equal("http://upload-dst.com"))

			// complete
			Ω(bbs.CompletedRunOnce.Guid).Should(Equal("run-once-guid"))
			Ω(bbs.CompletedRunOnce.ExecutorID).Should(Equal("fake-executor-id"))
			Ω(bbs.CompletedRunOnce.ContainerHandle).ShouldNot(BeZero())
		})
	})
})
