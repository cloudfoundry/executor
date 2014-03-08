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
	"github.com/cloudfoundry-incubator/executor/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/executor/logstreamer"
	"github.com/cloudfoundry-incubator/executor/logstreamer/fakelogstreamer"
	. "github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fakeuploader"
)

var _ = Describe("RunOnceTransformer", func() {
	var (
		backendPlugin      backend_plugin.BackendPlugin
		downloader         downloader.Downloader
		logger             *steno.Logger
		logStreamer        *fakelogstreamer.FakeLogStreamer
		uploader           uploader.Uploader
		wardenClient       *fake_gordon.FakeGordon
		runOnceTransformer *RunOnceTransformer
	)

	BeforeEach(func() {
		logStreamer = fakelogstreamer.New()
		backendPlugin = linuxplugin.New()
		downloader = &fakedownloader.FakeDownloader{}
		uploader = &fakeuploader.FakeUploader{}
		logger = &steno.Logger{}

		logStreamerFactory := func(models.LogConfig) logstreamer.LogStreamer {
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
			ContainerHandle: "some-container-handle",
		}

		Ω(runOnceTransformer.ActionsFor(&runOnce)).To(Equal([]action_runner.Action{
			run_action.New(
				&runOnce,
				runActionModel,
				logStreamer,
				backendPlugin,
				wardenClient,
				logger,
			),
			download_action.New(
				&runOnce,
				downloadActionModel,
				downloader,
				"/fake/temp/dir",
				backendPlugin,
				wardenClient,
				logger,
			),
			upload_action.New(
				&runOnce,
				uploadActionModel,
				uploader,
				"/fake/temp/dir",
				wardenClient,
				logger,
			),
			fetch_result_action.New(
				&runOnce,
				fetchResultActionModel,
				"/fake/temp/dir",
				wardenClient,
				logger,
			),
		}))
	})
})
