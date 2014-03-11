package run_once_transformer

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/actions/download_action"
	"github.com/cloudfoundry-incubator/executor/actions/fetch_result_action"
	"github.com/cloudfoundry-incubator/executor/actions/run_action"
	"github.com/cloudfoundry-incubator/executor/actions/upload_action"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/uploader"
)

type RunOnceTransformer struct {
	logStreamerFactory log_streamer_factory.LogStreamerFactory
	downloader         downloader.Downloader
	uploader           uploader.Uploader
	backendPlugin      backend_plugin.BackendPlugin
	wardenClient       gordon.Client
	logger             *steno.Logger
	tempDir            string
}

func NewRunOnceTransformer(
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
	tempDir string,
) *RunOnceTransformer {
	return &RunOnceTransformer{
		logStreamerFactory: logStreamerFactory,
		downloader:         downloader,
		uploader:           uploader,
		backendPlugin:      backendPlugin,
		wardenClient:       wardenClient,
		logger:             logger,
		tempDir:            tempDir,
	}
}

func (transformer *RunOnceTransformer) ActionsFor(
	runOnce *models.RunOnce,
) []action_runner.Action {
	logStreamer := transformer.logStreamerFactory(runOnce.Log)

	subActions := []action_runner.Action{}

	var subAction action_runner.Action

	for _, a := range runOnce.Actions {
		switch actionModel := a.Action.(type) {
		case models.RunAction:
			subAction = run_action.New(
				runOnce.ContainerHandle,
				actionModel,
				runOnce.FileDescriptors,
				logStreamer,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.DownloadAction:
			subAction = download_action.New(
				runOnce.ContainerHandle,
				actionModel,
				transformer.downloader,
				transformer.tempDir,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.UploadAction:
			subAction = upload_action.New(
				runOnce.ContainerHandle,
				actionModel,
				transformer.uploader,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.FetchResultAction:
			subAction = fetch_result_action.New(
				runOnce,
				actionModel,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
			)
		}

		subActions = append(subActions, subAction)
	}

	return subActions
}
