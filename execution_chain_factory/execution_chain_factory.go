package execution_chain_factory

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

type ExecutionChainFactory struct {
	streamer      logstreamer.LogStreamer
	downloader    downloader.Downloader
	uploader      uploader.Uploader
	backendPlugin backend_plugin.BackendPlugin
	wardenClient  gordon.Client
	logger        *steno.Logger
	tempDir       string
}

func NewExecutionChainFactory(
	streamer logstreamer.LogStreamer,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
	tempDir string,
) *ExecutionChainFactory {
	return &ExecutionChainFactory{
		streamer:      streamer,
		downloader:    downloader,
		uploader:      uploader,
		backendPlugin: backendPlugin,
		wardenClient:  wardenClient,
		logger:        logger,
		tempDir:       tempDir,
	}
}

func (factory *ExecutionChainFactory) NewChain(
	actionModels []models.ExecutorAction,
	runOnce *models.RunOnce,
	containerHandle string,
) []action_runner.Action {
	subActions := []action_runner.Action{}
	var subAction action_runner.Action
	for _, a := range actionModels {
		switch actionModel := a.Action.(type) {
		case models.RunAction:
			subAction = run_action.New(
				actionModel,
				containerHandle,
				factory.streamer,
				factory.backendPlugin,
				factory.wardenClient,
				factory.logger,
			)
		case models.DownloadAction:
			subAction = download_action.New(
				actionModel,
				containerHandle,
				factory.downloader,
				factory.tempDir,
				factory.backendPlugin,
				factory.wardenClient,
				factory.logger,
			)
		case models.UploadAction:
			subAction = upload_action.New(
				actionModel,
				containerHandle,
				factory.uploader,
				factory.tempDir,
				factory.wardenClient,
				factory.logger,
			)
		case models.FetchResultAction:
			subAction = fetch_result_action.New(
				runOnce,
				actionModel,
				containerHandle,
				factory.tempDir,
				factory.wardenClient,
				factory.logger,
			)
		}

		subActions = append(subActions, subAction)
	}

	return subActions
}
