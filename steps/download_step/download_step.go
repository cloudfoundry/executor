package download_step

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/bytefmt"
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

func (step *DownloadStep) Perform() (err error) {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.containerHandle,
		},
		"runonce.handle.download-action",
	)

	fmt.Fprintf(step.streamer.Stdout(), "Downloading %s\n", step.model.Name)
	defer func() {
		if err != nil {
			fmt.Fprintf(step.streamer.Stderr(), "Downloading %s failed\n", step.model.Name)
		}
	}()

	url, err := url.ParseRequestURI(step.model.From)
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

	downloadSize, err := step.downloader.Download(url, downloadedFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(step.streamer.Stdout(), "Downloaded %s (%s)\n", step.model.Name, bytefmt.ByteSize(uint64(downloadSize)))

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
