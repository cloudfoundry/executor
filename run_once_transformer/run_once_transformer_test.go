package run_once_transformer_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/compressor"
	"github.com/cloudfoundry-incubator/executor/compressor/fake_compressor"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry-incubator/executor/extractor/fake_extractor"
	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
	"github.com/cloudfoundry-incubator/executor/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
)

var _ = Describe("RunOnceTransformer", func() {
	var (
		backendPlugin      backend_plugin.BackendPlugin
		downloader         downloader.Downloader
		logger             *steno.Logger
		logStreamer        *fake_log_streamer.FakeLogStreamer
		uploader           uploader.Uploader
		extractor          extractor.Extractor
		compressor         compressor.Compressor
		wardenClient       *fake_gordon.FakeGordon
		runOnceTransformer *RunOnceTransformer
		handle             string
		result             string
	)

	BeforeEach(func() {
		logStreamer = fake_log_streamer.New()
		backendPlugin = linux_plugin.New()
		downloader = &fake_downloader.FakeDownloader{}
		uploader = &fake_uploader.FakeUploader{}
		extractor = &fake_extractor.FakeExtractor{}
		compressor = &fake_compressor.FakeCompressor{}
		logger = &steno.Logger{}
		handle = "some-handle"

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return logStreamer
		}

		runOnceTransformer = NewRunOnceTransformer(
			logStreamerFactory,
			downloader,
			uploader,
			extractor,
			compressor,
			backendPlugin,
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

		runOnce := models.RunOnce{
			Guid: "some-guid",
			Actions: []models.ExecutorAction{
				{runActionModel},
				{downloadActionModel},
				{uploadActionModel},
				{fetchResultActionModel},
			},
			FileDescriptors: 117,
		}

		Ω(runOnceTransformer.StepsFor(&runOnce, &handle, &result)).To(Equal([]sequence.Step{
			run_step.New(
				handle,
				runActionModel,
				117,
				logStreamer,
				backendPlugin,
				wardenClient,
				logger,
			),
			download_step.New(
				handle,
				downloadActionModel,
				downloader,
				extractor,
				"/fake/temp/dir",
				backendPlugin,
				wardenClient,
				logger,
			),
			upload_step.New(
				handle,
				uploadActionModel,
				uploader,
				compressor,
				"/fake/temp/dir",
				wardenClient,
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
		}))
	})
})
