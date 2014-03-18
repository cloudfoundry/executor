package run_once_transformer_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/actions/download_action"
	"github.com/cloudfoundry-incubator/executor/actions/fetch_result_action"
	"github.com/cloudfoundry-incubator/executor/actions/run_action"
	"github.com/cloudfoundry-incubator/executor/actions/upload_action"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/run_once_transformer"
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
		logger = &steno.Logger{}
		handle = "some-handle"

		logStreamerFactory := func(models.LogConfig) log_streamer.LogStreamer {
			return logStreamer
		}

		runOnceTransformer = NewRunOnceTransformer(
			logStreamerFactory,
			downloader,
			uploader,
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

		Ω(runOnceTransformer.ActionsFor(&runOnce, &handle, &result)).To(Equal([]action_runner.Action{
			run_action.New(
				handle,
				runActionModel,
				117,
				logStreamer,
				backendPlugin,
				wardenClient,
				logger,
			),
			download_action.New(
				handle,
				downloadActionModel,
				downloader,
				"/fake/temp/dir",
				backendPlugin,
				wardenClient,
				logger,
			),
			upload_action.New(
				handle,
				uploadActionModel,
				uploader,
				"/fake/temp/dir",
				wardenClient,
				logger,
			),
			fetch_result_action.New(
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
