package download_step

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type DownloadStep struct {
	containerHandle string
	model           models.DownloadAction
	downloader      downloader.Downloader
	extractor       extractor.Extractor
	tempDir         string
	backendPlugin   backend_plugin.BackendPlugin
	wardenClient    gordon.Client
	streamer        log_streamer.LogStreamer
	logger          *steno.Logger
}

func New(
	containerHandle string,
	model models.DownloadAction,
	downloader downloader.Downloader,
	extractor extractor.Extractor,
	tempDir string,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	streamer log_streamer.LogStreamer,
	logger *steno.Logger,
) *DownloadStep {
	return &DownloadStep{
		containerHandle: containerHandle,
		model:           model,
		downloader:      downloader,
		extractor:       extractor,
		tempDir:         tempDir,
		backendPlugin:   backendPlugin,
		wardenClient:    wardenClient,
		streamer:        streamer,
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

	if step.streamer != nil {
		step.streamer.StreamStdout(fmt.Sprintf("Downloaded %s", step.model.Name))
	}

	if step.model.Extract {
		extractionDir, err := ioutil.TempDir(step.tempDir, "extracted")
		if err != nil {
			return err
		}

		defer os.RemoveAll(extractionDir)

		err = step.extractor.Extract(downloadedFile.Name(), extractionDir)
		if err != nil {
			info, _ := downloadedFile.Stat()

			body, _ := ioutil.ReadAll(downloadedFile)

			step.logger.Warnd(
				map[string]interface{}{
					"error": err.Error(),
					"url":   step.model.From,
					"info":  info.Size(),
					"body":  string(body),
				},
				"downloader.extract-failed",
			)

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
