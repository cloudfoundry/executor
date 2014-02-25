package actionrunner

import (
	"fmt"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ActionRunnerInterface interface {
	Run(containerHandle string, streamer logstreamer.LogStreamer, actions []models.ExecutorAction) (result string, err error)
}

type BackendPlugin interface {
	BuildRunScript(models.RunAction) string
	BuildCreateDirectoryRecursivelyCommand(string) string
}

type ActionRunner struct {
	wardenClient  gordon.Client
	backendPlugin BackendPlugin
	downloader    downloader.Downloader
	uploader      uploader.Uploader
	tempDir       string
	logger        *steno.Logger
}

type RunActionTimeoutError struct {
	Action models.RunAction
}

func (e RunActionTimeoutError) Error() string {
	return fmt.Sprintf("action timed out after %s", e.Action.Timeout)
}

func New(wardenClient gordon.Client, backendPlugin BackendPlugin, downloader downloader.Downloader, uploader uploader.Uploader, tempDir string, logger *steno.Logger) *ActionRunner {
	return &ActionRunner{
		wardenClient:  wardenClient,
		backendPlugin: backendPlugin,
		downloader:    downloader,
		uploader:      uploader,
		tempDir:       tempDir,
		logger:        logger,
	}
}

func (runner *ActionRunner) Run(containerHandle string, streamer logstreamer.LogStreamer, actions []models.ExecutorAction) (string, error) {
	result := ""
	for _, action := range actions {
		var err error
		switch a := action.Action.(type) {
		case models.RunAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.run-action")
			err = runner.performRunAction(containerHandle, streamer, a)
		case models.DownloadAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.download-action")
			err = runner.performDownloadAction(containerHandle, a)
		case models.UploadAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.upload-action")
			err = runner.performUploadAction(containerHandle, a)
		case models.FetchResultAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.fetch-result-action")
			result, err = runner.performFetchResultAction(containerHandle, a)
		}
		if err != nil {
			return "", err
		}
	}

	return result, nil
}

func (runner *ActionRunner) performRunAction(containerHandle string, streamer logstreamer.LogStreamer, action models.RunAction) error {
	runRunner := NewRunRunner(runner.wardenClient, runner.backendPlugin)
	return runRunner.perform(containerHandle, streamer, action)
}

func (runner *ActionRunner) performDownloadAction(containerHandle string, action models.DownloadAction) error {
	downloadRunner := NewDownloadRunner(runner.downloader, runner.wardenClient, runner.tempDir, runner.backendPlugin)
	return downloadRunner.perform(containerHandle, action)
}

func (runner *ActionRunner) performUploadAction(containerHandle string, action models.UploadAction) error {
	uploadRunner := NewUploadRunner(runner.uploader, runner.wardenClient, runner.tempDir)
	return uploadRunner.perform(containerHandle, action)
}

func (runner *ActionRunner) performFetchResultAction(containerHandle string, action models.FetchResultAction) (string, error) {
	fetchResultRunner := NewFetchResultRunner(runner.wardenClient, runner.tempDir)
	return fetchResultRunner.perform(containerHandle, action)
}
