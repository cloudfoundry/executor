package task_handler_test

import (
	"errors"
	"io/ioutil"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.google.com/p/gogoprotobuf/proto"
	warden "github.com/cloudfoundry-incubator/garden/protocol"
	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/task_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry/fake_task_registry"
	"github.com/cloudfoundry-incubator/executor/task_transformer"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
)

var _ = Describe("TaskHandler", func() {
	var (
		handler *TaskHandler
		task    *models.Task
		cancel  chan struct{}

		bbs                 *fake_bbs.FakeExecutorBBS
		wardenClient        *fake_gordon.FakeGordon
		downloader          *fake_downloader.FakeDownloader
		uploader            *fake_uploader.FakeUploader
		extractor           *fake_extractor.FakeExtractor
		compressor          *fake_compressor.FakeCompressor
		transformer         *task_transformer.TaskTransformer
		taskRegistry        *fake_task_registry.FakeTaskRegistry
		containerInodeLimit int
	)

	BeforeEach(func() {
		cancel = make(chan struct{})

		task = &models.Task{
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

		bbs = fake_bbs.NewFakeExecutorBBS()
		wardenClient = fake_gordon.New()
		taskRegistry = fake_task_registry.New()

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return fake_log_streamer.New()
		}

		logger := steno.NewLogger("test-logger")

		containerInodeLimit = 200000
		downloader = &fake_downloader.FakeDownloader{}
		uploader = &fake_uploader.FakeUploader{}
		extractor = &fake_extractor.FakeExtractor{}
		compressor = &fake_compressor.FakeCompressor{}

		tmpDir, err := ioutil.TempDir("", "run-once-handler-tmp")
		Ω(err).ShouldNot(HaveOccurred())

		transformer = task_transformer.NewTaskTransformer(
			logStreamerFactory,
			downloader,
			uploader,
			extractor,
			compressor,
			wardenClient,
			logger,
			tmpDir,
		)

		handler = New(
			bbs,
			wardenClient,
			"container-owner-name",
			taskRegistry,
			transformer,
			logStreamerFactory,
			logger,
			containerInodeLimit,
		)
	})

	Describe("Task", func() {
		setUpSuccessfulRuns := func() {
			processPayloadStream := make(chan *warden.ProcessPayload, 1000)

			wardenClient.SetRunReturnValues(0, processPayloadStream, nil)

			successfulExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(0)}

			go func() {
				processPayloadStream <- successfulExit
			}()
		}

		setUpFailedRuns := func() {
			processPayloadStream := make(chan *warden.ProcessPayload, 1000)

			wardenClient.SetRunReturnValues(0, processPayloadStream, nil)

			failedExit := &warden.ProcessPayload{ExitStatus: proto.Uint32(3)}

			go func() {
				processPayloadStream <- failedExit
			}()
		}

		Context("when the run once succeeds", func() {
			BeforeEach(setUpSuccessfulRuns)

			It("registers, claims, creates container, starts, (executes...), completes", func() {
				originalTask := task
				handler.Task(task, "fake-executor-id", cancel)

				// register
				Ω(taskRegistry.RegisteredTasks).Should(ContainElement(originalTask))

				// claim
				claimed := bbs.ClaimedTasks()
				Ω(claimed).ShouldNot(BeEmpty())
				Ω(claimed[0].Guid).Should(Equal("run-once-guid"))
				Ω(claimed[0].ExecutorID).Should(Equal("fake-executor-id"))

				// create container
				handles := wardenClient.CreatedHandles()
				Ω(handles).Should(HaveLen(1))

				handle := handles[0]

				// container must have owner
				properties := wardenClient.CreatedProperties(handle)
				Ω(properties["owner"]).Should(Equal("container-owner-name"))

				//limit memoru & disk
				Ω(wardenClient.MemoryLimits()[0].Handle).Should(Equal(handle))
				Ω(wardenClient.MemoryLimits()[0].Limit).Should(BeNumerically("==", 512*1024*1024))
				Ω(wardenClient.DiskLimits()[0].Handle).Should(Equal(handle))
				Ω(wardenClient.DiskLimits()[0].Limits.ByteLimit).Should(BeNumerically("==", 1024*1024*1024))
				Ω(wardenClient.DiskLimits()[0].Limits.InodeLimit).Should(BeNumerically("==", containerInodeLimit))

				// start
				started := bbs.StartedTasks()
				Ω(started).ShouldNot(BeEmpty())
				Ω(started[0].Guid).Should(Equal("run-once-guid"))
				Ω(started[0].ExecutorID).Should(Equal("fake-executor-id"))
				Ω(started[0].ContainerHandle).Should(Equal(handle))

				// execute download step
				Ω(downloader.DownloadedUrls).ShouldNot(BeEmpty())
				Ω(downloader.DownloadedUrls[0].String()).Should(Equal("http://download-src.com"))

				// execute run step
				ranScripts := []string{}
				for _, script := range wardenClient.ScriptsThatRan() {
					Ω(script.Handle).Should(Equal(started[0].ContainerHandle))

					ranScripts = append(ranScripts, script.Script)
				}

				Ω(ranScripts).Should(ContainElement("sudo reboot"))

				// execute upload step
				Ω(uploader.UploadUrls).ShouldNot(BeEmpty())
				Ω(uploader.UploadUrls[0].String()).Should(Equal("http://upload-dst.com"))

				// complete
				completed := bbs.CompletedTasks()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal("run-once-guid"))
				Ω(completed[0].ExecutorID).Should(Equal("fake-executor-id"))
				Ω(completed[0].ContainerHandle).ShouldNot(BeZero())
				Ω(completed[0].Failed).Should(BeFalse())
				Ω(completed[0].FailureReason).Should(BeZero())
			})
		})

		Context("when the run once fails", func() {
			BeforeEach(setUpFailedRuns)

			It("registers, claims, creates container, starts, (executes...), completes (failure)", func() {
				originalTask := task
				handler.Task(task, "fake-executor-id", cancel)

				// register
				Ω(taskRegistry.RegisteredTasks).Should(ContainElement(originalTask))

				// claim
				claimed := bbs.ClaimedTasks()
				Ω(claimed).ShouldNot(BeEmpty())
				Ω(claimed[0].Guid).Should(Equal("run-once-guid"))
				Ω(claimed[0].ExecutorID).Should(Equal("fake-executor-id"))

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
				started := bbs.StartedTasks()
				Ω(started).ShouldNot(BeEmpty())
				Ω(started[0].Guid).Should(Equal("run-once-guid"))
				Ω(started[0].ExecutorID).Should(Equal("fake-executor-id"))
				Ω(started[0].ContainerHandle).Should(Equal(handle))

				// execute download step
				Ω(downloader.DownloadedUrls).ShouldNot(BeEmpty())
				Ω(downloader.DownloadedUrls[0].String()).Should(Equal("http://download-src.com"))

				// execute run step
				ranScripts := []string{}
				for _, script := range wardenClient.ScriptsThatRan() {
					Ω(script.Handle).Should(Equal(started[0].ContainerHandle))

					ranScripts = append(ranScripts, script.Script)
				}

				Ω(ranScripts).Should(ContainElement("sudo reboot"))

				// no upload step, as the execute step fails
				Ω(uploader.UploadUrls).Should(BeEmpty())

				// complete
				completed := bbs.CompletedTasks()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Guid).Should(Equal("run-once-guid"))
				Ω(completed[0].ExecutorID).Should(Equal("fake-executor-id"))
				Ω(completed[0].ContainerHandle).ShouldNot(BeZero())
				Ω(completed[0].Failed).Should(BeTrue())
				Ω(completed[0].FailureReason).Should(MatchRegexp(`\b3\b`))
			})
		})

		Context("when told to cancel", func() {
			var running chan struct{}

			BeforeEach(func() {
				setUpSuccessfulRuns()
				running = make(chan struct{})

				wardenClient.WhenRunning("", "sudo reboot", gordon.ResourceLimits{}, []gordon.EnvironmentVariable{}, func() (uint32, <-chan *warden.ProcessPayload, error) {
					running <- struct{}{}
					time.Sleep(1 * time.Hour)
					return 0, nil, nil
				})
			})

			It("does not continue performing", func() {
				done := make(chan struct{})

				go func() {
					handler.Task(task, "fake-executor-id", cancel)
					done <- struct{}{}
				}()

				Eventually(running).Should(Receive())

				close(cancel)

				Eventually(done).Should(Receive())

				Ω(wardenClient.StoppedHandles()).ShouldNot(BeEmpty())

				Ω(uploader.UploadUrls).Should(BeEmpty())

				completed := bbs.CompletedTasks()
				Ω(completed).ShouldNot(BeEmpty())
				Ω(completed[0].Failed).Should(BeTrue())
				Ω(completed[0].FailureReason).Should(ContainSubstring("cancelled"))
			})
		})
	})

	Describe("Cleanup", func() {
		Context("when there are containers matching the owner name", func() {
			BeforeEach(func() {
				wardenClient.WhenListing(
					func(properties map[string]string) (*warden.ListResponse, error) {
						Ω(properties["owner"]).Should(Equal("container-owner-name"))

						return &warden.ListResponse{
							Handles: []string{"doomed-handle-1", "doomed-handle-2"},
						}, nil
					},
				)
			})

			It("destroys them", func() {
				err := handler.Cleanup()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(wardenClient.DestroyedHandles()).Should(Equal([]string{
					"doomed-handle-1",
					"doomed-handle-2",
				}))
			})

			Context("when destroying a container fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.DestroyError = disaster
				})

				It("returns the error", func() {
					err := handler.Cleanup()
					Ω(err).Should(Equal(disaster))
				})
			})
		})

		Context("when listing containers fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.WhenListing(
					func(properties map[string]string) (*warden.ListResponse, error) {
						return nil, disaster
					},
				)
			})

			It("returns the error", func() {
				err := handler.Cleanup()
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
