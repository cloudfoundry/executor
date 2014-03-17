package run_once_handler_test

import (
	"github.com/vito/gordon"
	"io/ioutil"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	. "github.com/cloudfoundry-incubator/executor/run_once_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry/fake_task_registry"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
)

var _ = Describe("RunOnceHandler", func() {
	Describe("RunOnce", func() {
		var (
			handler *RunOnceHandler
			runOnce models.RunOnce
			cancel  chan struct{}

			bbs                 *fakebbs.FakeExecutorBBS
			wardenClient        *fake_gordon.FakeGordon
			downloader          *fake_downloader.FakeDownloader
			uploader            *fake_uploader.FakeUploader
			transformer         *run_once_transformer.RunOnceTransformer
			taskRegistry        *fake_task_registry.FakeTaskRegistry
			containerInodeLimit int
		)

		BeforeEach(func() {
			cancel = make(chan struct{})

			runOnce = models.RunOnce{
				Guid:     "run-once-guid",
				MemoryMB: 512,
				DiskMB:   1024,
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
			taskRegistry = fake_task_registry.New()

			logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
				return nil
			}

			logger := steno.NewLogger("test-logger")

			containerInodeLimit = 200000
			backendPlugin := linux_plugin.New()
			downloader = &fake_downloader.FakeDownloader{}
			uploader = &fake_uploader.FakeUploader{}

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
				containerInodeLimit,
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
			handler.RunOnce(runOnce, "fake-executor-id", cancel)

			// register
			Ω(taskRegistry.RegisteredRunOnces).Should(ContainElement(runOnce))

			// claim
			Ω(bbs.ClaimedRunOnce.Guid).Should(Equal("run-once-guid"))
			Ω(bbs.ClaimedRunOnce.ExecutorID).Should(Equal("fake-executor-id"))

			// create container
			Ω(wardenClient.CreatedHandles()).ShouldNot(BeEmpty())
			handle := wardenClient.CreatedHandles()[0]

			//limit memoru & disk
			Ω(wardenClient.MemoryLimits()[0].Handle).Should(Equal(handle))
			Ω(wardenClient.MemoryLimits()[0].Limit).Should(BeNumerically("==", 512*1024*1024))
			Ω(wardenClient.DiskLimits()[0].Handle).Should(Equal(handle))
			Ω(wardenClient.DiskLimits()[0].Limits.ByteLimit).Should(BeNumerically("==", 1024*1024*1024))
			Ω(wardenClient.DiskLimits()[0].Limits.InodeLimit).Should(BeNumerically("==", containerInodeLimit))

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

		Context("when told to cancel", func() {
			var running chan struct{}

			BeforeEach(func() {
				running = make(chan struct{})

				wardenClient.WhenRunning("", "sudo reboot", gordon.ResourceLimits{}, func() (uint32, <-chan *warden.ProcessPayload, error) {
					running <- struct{}{}
					time.Sleep(1 * time.Hour)
					return 0, nil, nil
				})
			})

			It("does not continue performing", func() {
				done := make(chan struct{})

				go func() {
					handler.RunOnce(runOnce, "fake-executor-id", cancel)
					done <- struct{}{}
				}()

				Eventually(running).Should(Receive())

				close(cancel)

				Eventually(done).Should(Receive())

				Ω(wardenClient.StoppedHandles()).ShouldNot(BeEmpty())

				Ω(uploader.UploadUrls).Should(BeEmpty())

				Ω(bbs.CompletedRunOnce).Should(BeZero())
			})
		})
	})
})
