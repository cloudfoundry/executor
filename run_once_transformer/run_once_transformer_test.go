package run_once_transformer_test

import (
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/emit_progress_step"
	"github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
	"github.com/cloudfoundry-incubator/executor/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/steps/try_step"
	"github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
)

var _ = Describe("TaskTransformer", func() {
	var (
		downloader         downloader.Downloader
		logger             *steno.Logger
		logStreamer        *fake_log_streamer.FakeLogStreamer
		uploader           uploader.Uploader
		extractor          extractor.Extractor
		compressor         compressor.Compressor
		wardenClient       *fake_gordon.FakeGordon
		runOnceTransformer *TaskTransformer
		handle             string
		result             string
	)

	BeforeEach(func() {
		logStreamer = fake_log_streamer.New()
		downloader = &fake_downloader.FakeDownloader{}
		uploader = &fake_uploader.FakeUploader{}
		extractor = &fake_extractor.FakeExtractor{}
		compressor = &fake_compressor.FakeCompressor{}
		logger = &steno.Logger{}
		handle = "some-handle"

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return logStreamer
		}

		runOnceTransformer = NewTaskTransformer(
			logStreamerFactory,
			downloader,
			uploader,
			extractor,
			compressor,
			wardenClient,
			logger,
			"/fake/temp/dir",
		)
	})

	It("is correct", func() {
		runActionModel := models.RunAction{Script: "do-something"}
		downloadActionModel := models.DownloadAction{From: "/file/to/download"}
		uploadActionModel := models.UploadAction{From: "/file/to/upload"}
		fetchResultActionModel := models.FetchResultAction{File: "some-file"}
		tryActionModel := models.TryAction{Action: models.ExecutorAction{runActionModel}}
		emitProgressActionModel := models.EmitProgressAction{
			Action:         models.ExecutorAction{runActionModel},
			StartMessage:   "starting",
			SuccessMessage: "successing",
			FailureMessage: "failuring",
		}

		runOnce := models.Task{
			Guid: "some-guid",
			Actions: []models.ExecutorAction{
				{runActionModel},
				{downloadActionModel},
				{uploadActionModel},
				{fetchResultActionModel},
				{tryActionModel},
				{emitProgressActionModel},
			},
			FileDescriptors: 117,
		}

		Ω(runOnceTransformer.StepsFor(&runOnce, handle, &result)).To(Equal([]sequence.Step{
			run_step.New(
				handle,
				runActionModel,
				117,
				logStreamer,
				wardenClient,
				logger,
			),
			download_step.New(
				handle,
				downloadActionModel,
				downloader,
				extractor,
				"/fake/temp/dir",
				wardenClient,
				logStreamer,
				logger,
			),
			upload_step.New(
				handle,
				uploadActionModel,
				uploader,
				compressor,
				"/fake/temp/dir",
				wardenClient,
				logStreamer,
				logger,
			),
			fetch_result_step.New(
				handle,
				fetchResultActionModel,
				"/fake/temp/dir",
				wardenClient,
				logger,
				&result,
			),
			try_step.New(
				run_step.New(
					handle,
					runActionModel,
					117,
					logStreamer,
					wardenClient,
					logger,
				),
				logger,
			),
			emit_progress_step.New(
				run_step.New(
					handle,
					runActionModel,
					117,
					logStreamer,
					wardenClient,
					logger,
				),
				"starting",
				"successing",
				"failuring",
				logStreamer,
				logger,
			),
		}))
	})
})
