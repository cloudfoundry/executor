package run_once_transformer

import (
	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/download_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/fetch_result_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/run_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/upload_action"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type RunOnceTransformer struct {
	streamer      logstreamer.LogStreamer
	downloader    downloader.Downloader
	uploader      uploader.Uploader
	backendPlugin backend_plugin.BackendPlugin
	wardenClient  gordon.Client
	logger        *steno.Logger
	tempDir       string
}

func NewRunOnceTransformer(
	streamer logstreamer.LogStreamer,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
	tempDir string,
) *RunOnceTransformer {
	return &RunOnceTransformer{
		streamer:      streamer,
		downloader:    downloader,
		uploader:      uploader,
		backendPlugin: backendPlugin,
		wardenClient:  wardenClient,
		logger:        logger,
		tempDir:       tempDir,
	}
}

func (transformer *RunOnceTransformer) ActionsFor(
	runOnce *models.RunOnce,
) []action_runner.Action {
	subActions := []action_runner.Action{}

	var subAction action_runner.Action

	for _, a := range runOnce.Actions {
		switch actionModel := a.Action.(type) {
		case models.RunAction:
			subAction = run_action.New(
				actionModel,
				runOnce.ContainerHandle,
				transformer.streamer,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.DownloadAction:
			subAction = download_action.New(
				actionModel,
				runOnce.ContainerHandle,
				transformer.downloader,
				transformer.tempDir,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.UploadAction:
			subAction = upload_action.New(
				actionModel,
				runOnce.ContainerHandle,
				transformer.uploader,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.FetchResultAction:
			subAction = fetch_result_action.New(
				runOnce,
				actionModel,
				runOnce.ContainerHandle,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
			)
		}

		subActions = append(subActions, subAction)
	}

	return subActions
}
