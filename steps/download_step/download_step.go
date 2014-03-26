package download_step

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type DownloadStep struct {
	containerHandle string
	model           models.DownloadAction
	downloader      downloader.Downloader
	tempDir         string
	backendPlugin   backend_plugin.BackendPlugin
	wardenClient    gordon.Client
	logger          *steno.Logger
}

func New(
	containerHandle string,
	model models.DownloadAction,
	downloader downloader.Downloader,
	tempDir string,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *DownloadStep {
	return &DownloadStep{
		containerHandle: containerHandle,
		model:           model,
		downloader:      downloader,
		tempDir:         tempDir,
		backendPlugin:   backendPlugin,
		wardenClient:    wardenClient,
		logger:          logger,
	}
}

func (step *DownloadStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.containerHandle,
		},
		"runonce.handle.download-action",
	)

	url, err := url.Parse(step.model.From)
	if err != nil {
		return err
	}

	downloadedFile, err := ioutil.TempFile(step.tempDir, "downloaded")
	if err != nil {
		return err
	}
	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	err = step.downloader.Download(url, downloadedFile)
	if err != nil {
		return err
	}

	if step.model.Extract {
		extractionDir, err := ioutil.TempDir(step.tempDir, "extracted")
		if err != nil {
			return err
		}

		err = extractor.Extract(downloadedFile.Name(), extractionDir)
		defer os.RemoveAll(extractionDir)
		if err != nil {
			return err
		}

		return step.copyExtractedFiles(extractionDir, step.model.To)
	} else {
		_, err = step.wardenClient.CopyIn(step.containerHandle, downloadedFile.Name(), step.model.To)
		return err
	}
}

func (step *DownloadStep) Cancel() {}

func (step *DownloadStep) Cleanup() {}

func (step *DownloadStep) copyExtractedFiles(source string, destination string) error {
	_, err := step.wardenClient.CopyIn(
		step.containerHandle,
		source+string(filepath.Separator),
		destination+string(filepath.Separator),
	)

	return err
}
