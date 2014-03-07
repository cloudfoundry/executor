package actionrunner

import (
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/fetch_result_action"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/download_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/run_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/upload_action"
)

type ActionRunnerInterface interface {
	Run(*models.RunOnce, logstreamer.LogStreamer, []models.ExecutorAction) (string, error)
}

type ActionRunner struct {
	wardenClient  gordon.Client
	backendPlugin backend_plugin.BackendPlugin
	downloader    downloader.Downloader
	uploader      uploader.Uploader
	tempDir       string
	logger        *steno.Logger
}

func New(
	wardenClient gordon.Client,
	backendPlugin backend_plugin.BackendPlugin,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	tempDir string,
	logger *steno.Logger,
) *ActionRunner {
	return &ActionRunner{
		wardenClient:  wardenClient,
		backendPlugin: backendPlugin,
		downloader:    downloader,
		uploader:      uploader,
		tempDir:       tempDir,
		logger:        logger,
	}
}

func (runner *ActionRunner) Run(runOnce *models.RunOnce, streamer logstreamer.LogStreamer, actions []models.ExecutorAction) (string, error) {
	result := ""
	for _, action := range actions {
		var err error
		switch a := action.Action.(type) {
		case models.RunAction:
			runAction := run_action.New(
				runOnce,
				a,
				streamer,
				runner.backendPlugin,
				runner.wardenClient,
				runner.logger,
			)

			results := make(chan error, 1)
			runAction.Perform(results)

			err = <-results
		case models.DownloadAction:
			runAction := download_action.New(
				runOnce,
				a,
				runner.downloader,
				runner.tempDir,
				runner.backendPlugin,
				runner.wardenClient,
				runner.logger,
			)

			results := make(chan error, 1)
			runAction.Perform(results)

			err = <-results
		case models.UploadAction:
			runAction := upload_action.New(
				runOnce,
				a,
				runner.uploader,
				runner.tempDir,
				runner.wardenClient,
				runner.logger,
			)

			results := make(chan error, 1)
			runAction.Perform(results)

			err = <-results
		case models.FetchResultAction:
			runAction := fetch_result_action.New(
				runOnce,
				a,
				runner.tempDir,
				runner.wardenClient,
				runner.logger,
			)

			results := make(chan error, 1)
			runAction.Perform(results)

			err = <-results
			result = runOnce.Result
		}
		if err != nil {
			return "", err
		}
	}
	return result, nil
}
