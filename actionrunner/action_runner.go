package actionrunner

import (
	"fmt"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/emitter"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ActionRunnerInterface interface {
	Run(containerHandle string, emitter emitter.Emitter, actions []models.ExecutorAction) error
}

type BackendPlugin interface {
	BuildRunScript(models.RunAction) string
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

func (runner *ActionRunner) Run(containerHandle string, emitter emitter.Emitter, actions []models.ExecutorAction) error {
	for _, action := range actions {
		var err error
		switch a := action.Action.(type) {
		case models.RunAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.run-action")
			err = runner.performRunAction(containerHandle, emitter, a)
		case models.DownloadAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.download-action")
			err = runner.performDownloadAction(containerHandle, a)
		case models.UploadAction:
			runner.logger.Infod(map[string]interface{}{"handle": containerHandle}, "runonce.handle.upload-action")
			err = runner.performUploadAction(containerHandle, a)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (runner *ActionRunner) performRunAction(containerHandle string, emitter emitter.Emitter, action models.RunAction) error {
	runRunner := NewRunRunner(runner.wardenClient, runner.backendPlugin)
	return runRunner.perform(containerHandle, emitter, action)
}

func (runner *ActionRunner) performDownloadAction(containerHandle string, action models.DownloadAction) error {
	downloadRunner := NewDownloadRunner(runner.downloader, runner.wardenClient, runner.tempDir)
	return downloadRunner.perform(containerHandle, action)
}

func (runner *ActionRunner) performUploadAction(containerHandle string, action models.UploadAction) error {
	uploadRunner := NewUploadRunner(runner.uploader, runner.wardenClient, runner.tempDir)
	return uploadRunner.perform(containerHandle, action)
}
