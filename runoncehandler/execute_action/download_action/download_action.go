package download_action

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type DownloadAction struct {
	model           models.DownloadAction
	containerHandle string
	downloader      downloader.Downloader
	tempDir         string
	backendPlugin   backend_plugin.BackendPlugin
	wardenClient    gordon.Client
	logger          *steno.Logger
}

func New(
	model models.DownloadAction,
	containerHandle string,
	downloader downloader.Downloader,
	tempDir string,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *DownloadAction {
	return &DownloadAction{
		model:           model,
		containerHandle: containerHandle,
		downloader:      downloader,
		tempDir:         tempDir,
		backendPlugin:   backendPlugin,
		wardenClient:    wardenClient,
		logger:          logger,
	}
}

func (action *DownloadAction) Perform(result chan<- error) {
	result <- action.perform()
}

func (action *DownloadAction) Cancel() {}

func (action *DownloadAction) Cleanup() {}

func (action *DownloadAction) perform() error {
	url, err := url.Parse(action.model.From)
	if err != nil {
		return err
	}

	downloadedFile, err := ioutil.TempFile(action.tempDir, "downloaded")
	if err != nil {
		return err
	}
	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	err = action.downloader.Download(url, downloadedFile)
	if err != nil {
		return err
	}

	createParentDirCommand := action.backendPlugin.BuildCreateDirectoryRecursivelyCommand(filepath.Dir(action.model.To))
	_, _, err = action.wardenClient.Run(action.containerHandle, createParentDirCommand)
	if err != nil {
		return err
	}

	if action.model.Extract {
		extractionDir, err := ioutil.TempDir(action.tempDir, "extracted")
		if err != nil {
			return err
		}

		err = extractor.Extract(downloadedFile.Name(), extractionDir)
		defer os.RemoveAll(extractionDir)
		if err != nil {
			return err
		}

		return action.copyExtractedFiles(extractionDir, action.model.To)
	} else {
		_, err = action.wardenClient.CopyIn(action.containerHandle, downloadedFile.Name(), action.model.To)
		return err
	}
}

func (action *DownloadAction) copyExtractedFiles(source string, destination string) error {
	_, err := action.wardenClient.CopyIn(
		action.containerHandle,
		source+string(filepath.Separator),
		destination+string(filepath.Separator),
	)

	return err
}
