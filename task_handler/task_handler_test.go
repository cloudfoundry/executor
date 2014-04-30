package task_handler_test

import (
	"errors"
	"io/ioutil"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
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
		wardenClient        *fake_warden_client.FakeClient
		downloader          *fake_downloader.FakeDownloader
		uploader            *fake_uploader.FakeUploader
		extractor           *fake_extractor.FakeExtractor
		compressor          *fake_compressor.FakeCompressor
		transformer         *task_transformer.TaskTransformer
		taskRegistry        *fake_task_registry.FakeTaskRegistry
		containerInodeLimit int
		maxCpuShares        int
	)

	handle := "some-container-handle"

	BeforeEach(func() {
		cancel = make(chan struct{})

		task = &models.Task{
			Guid:       "run-once-guid",
			MemoryMB:   512,
			DiskMB:     1024,
			CpuPercent: 25,
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
		wardenClient = fake_warden_client.New()
		taskRegistry = fake_task_registry.New()

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return fake_log_streamer.New()
		}

		logger := steno.NewLogger("test-logger")

		containerInodeLimit = 200000
		maxCpuShares = 1024
		downloader = &fake_downloader.FakeDownloader{}
		uploader = &fake_uploader.FakeUploader{}
		extractor = &fake_extractor.FakeExtractor{}
		compressor = &fake_compressor.FakeCompressor{}

		tmpDir, err := ioutil.TempDir("", "run-once-handler-tmp")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return handle, nil
		}

		transformer = task_transformer.NewTaskTransformer(
			logStreamerFactory,
			downloader,
			uploader,
			extractor,
			compressor,
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
			maxCpuShares,
		)
	})

	Describe("Task", func() {
		setUpSuccessfulRuns := func() {
			processPayloadStream := make(chan warden.ProcessStream, 1000)

			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				return 0, processPayloadStream, nil
			}

			exitStatus := uint32(0)
			successfulExit := warden.ProcessStream{ExitStatus: &exitStatus}

			go func() {
				processPayloadStream <- successfulExit
			}()
		}

		setUpFailedRuns := func() {
			processPayloadStream := make(chan warden.ProcessStream, 1000)

			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				return 0, processPayloadStream, nil
			}

			exitStatus := uint32(3)
			failedExit := warden.ProcessStream{ExitStatus: &exitStatus}

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
				createdSpecs := wardenClient.Connection.Created()
				Ω(createdSpecs).Should(HaveLen(1))

				createdSpec := createdSpecs[0]

				// container must have owner
				Ω(createdSpec.Properties["owner"]).Should(Equal("container-owner-name"))

				//limit memory, disk, and cpu
				Ω(wardenClient.Connection.LimitedMemory(handle)[0].LimitInBytes).Should(BeNumerically("==", 512*1024*1024))
				Ω(wardenClient.Connection.LimitedDisk(handle)[0].ByteLimit).Should(BeNumerically("==", 1024*1024*1024))
				Ω(wardenClient.Connection.LimitedDisk(handle)[0].InodeLimit).Should(BeNumerically("==", containerInodeLimit))
				Ω(wardenClient.Connection.LimitedCPU(handle)[0].LimitInShares).Should(BeNumerically("==", float64(maxCpuShares)*0.25))

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
				for _, spec := range wardenClient.Connection.SpawnedProcesses(handle) {
					ranScripts = append(ranScripts, spec.Script)
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
				Ω(completed[0].ContainerHandle).Should(Equal(handle))
				Ω(completed[0].Failed).Should(BeFalse())
				Ω(completed[0].FailureReason).Should(BeZero())
			})
		})

		Context("when the task fails at the register step", func() {
			BeforeEach(func() {
				taskRegistry.AddTaskErr = errors.New("boom")
				handler.Task(task, "fake-executor-id", cancel)
			})

			It("should not try to claim the task", func() {
				Ω(bbs.ClaimedTasks()).Should(BeEmpty())
			})

			It("should not complete the task", func() {
				Ω(bbs.CompletedTasks()).Should(BeEmpty())
			})
		})

		Context("when the bbs returns an error, when the claim is attempted", func() {
			BeforeEach(func() {
				bbs.SetClaimTaskErr(errors.New("boom"))
				handler.Task(task, "fake-executor-id", cancel)
			})

			It("should not create the container", func() {
				Ω(wardenClient.Connection.Created()).Should(BeEmpty())
			})

			It("should not complete the task", func() {
				Ω(bbs.CompletedTasks()).Should(BeEmpty())
			})
		})

		Context("when the task fails at the create container step", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
					return "", errors.New("thou shall not pass")
				}

				handler.Task(task, "fake-executor-id", cancel)
			})

			It("should not complete the task", func() {
				Ω(bbs.CompletedTasks()).Should(BeEmpty())
			})
		})

		Context("when the task fails at the limit container step", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenLimitingMemory = func(string, warden.MemoryLimits) (warden.MemoryLimits, error) {
					return warden.MemoryLimits{}, errors.New("thou shall not pass")
				}

				handler.Task(task, "fake-executor-id", cancel)
			})

			It("should not complete the task", func() {
				Ω(bbs.CompletedTasks()).Should(BeEmpty())
			})
		})

		Context("when the task fails at the start step", func() {
			BeforeEach(func() {
				bbs.SetStartTaskErr(errors.New("foo"))
				handler.Task(task, "fake-executor-id", cancel)
			})

			It("should not complete the task", func() {
				Ω(bbs.CompletedTasks()).Should(BeEmpty())
			})
		})

		Context("when the task fails at the execute step", func() {
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
				createdSpecs := wardenClient.Connection.Created()
				Ω(createdSpecs).Should(HaveLen(1))

				createdSpec := createdSpecs[0]

				// container must have owner
				Ω(createdSpec.Properties["owner"]).Should(Equal("container-owner-name"))

				//limit memory, disk, and cpu
				Ω(wardenClient.Connection.LimitedMemory(handle)[0].LimitInBytes).Should(BeNumerically("==", 512*1024*1024))
				Ω(wardenClient.Connection.LimitedDisk(handle)[0].ByteLimit).Should(BeNumerically("==", 1024*1024*1024))
				Ω(wardenClient.Connection.LimitedDisk(handle)[0].InodeLimit).Should(BeNumerically("==", containerInodeLimit))
				Ω(wardenClient.Connection.LimitedCPU(handle)[0].LimitInShares).Should(BeNumerically("==", float64(maxCpuShares)*0.25))

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
				for _, spec := range wardenClient.Connection.SpawnedProcesses(handle) {
					ranScripts = append(ranScripts, spec.Script)
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

				wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
					running <- struct{}{}
					time.Sleep(1 * time.Hour)
					return 0, nil, nil
				}
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

				Ω(wardenClient.Connection.Stopped(handle)).ShouldNot(BeEmpty())

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
				wardenClient.Connection.WhenListing = func(properties warden.Properties) ([]string, error) {
					Ω(properties["owner"]).Should(Equal("container-owner-name"))
					return []string{"doomed-handle-1", "doomed-handle-2"}, nil
				}
			})

			It("destroys them", func() {
				err := handler.Cleanup()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(wardenClient.Connection.Destroyed()).Should(Equal([]string{
					"doomed-handle-1",
					"doomed-handle-2",
				}))
			})

			Context("when destroying a container fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenDestroying = func(string) error {
						return disaster
					}
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
				wardenClient.Connection.WhenListing = func(properties warden.Properties) ([]string, error) {
					return nil, disaster
				}
			})

			It("returns the error", func() {
				err := handler.Cleanup()
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
