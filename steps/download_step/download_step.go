package download_step

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/gordon"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/bytefmt"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/extractor"
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

	downloadedFile, err := step.download()
	if err != nil {
		if step.model.DownloadFailureMessage != "" {
			fmt.Fprintf(step.streamer.Stderr(), "%s\n", step.model.DownloadFailureMessage)
		} else {
			fmt.Fprintf(step.streamer.Stderr(), "Downloading %s failed\n", step.model.Name)
		}
		return err
	}

	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	if step.model.Extract {
		extractionDir, err := step.extract(downloadedFile)
		if err != nil {
			fmt.Fprintf(step.streamer.Stderr(), "Extracting %s failed\n", step.model.Name)
			return err
		}

		defer os.RemoveAll(extractionDir)

		err = step.copyExtractedFiles(extractionDir, step.model.To)
		if err != nil {
			fmt.Fprintf(step.streamer.Stderr(), "Copying %s into the container failed\n", step.model.Name)
		}
		return err
	} else {
		_, err = step.wardenClient.CopyIn(step.containerHandle, downloadedFile.Name(), step.model.To)
		if err != nil {
			fmt.Fprintf(step.streamer.Stderr(), "Copying %s into the container failed\n", step.model.Name)
		}
		return err
	}
}

func (step *DownloadStep) download() (downloadedFile *os.File, err error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		return
	}

	downloadedFile, err = ioutil.TempFile(step.tempDir, "downloaded")
	if err != nil {
		return
	}

	downloadSize, err := step.downloader.Download(url, downloadedFile)
	if err != nil {
		return
	}

	fmt.Fprintf(step.streamer.Stdout(), "Downloaded %s (%s)\n", step.model.Name, bytefmt.ByteSize(uint64(downloadSize)))
	return
}

func (step *DownloadStep) extract(downloadedFile *os.File) (string, error) {
	extractionDir, err := ioutil.TempDir(step.tempDir, "extracted")
	if err != nil {
		return "", err
	}

	err = step.extractor.Extract(downloadedFile.Name(), extractionDir)
	if err != nil {
		info, statErr := downloadedFile.Stat()
		if statErr != nil {
			step.logger.Warnd(
				map[string]interface{}{
					"error":      err.Error(),
					"stat-error": statErr.Error(),
					"url":        step.model.From,
				},
				"downloader.extracted-stat-failed",
			)
			return "", err
		}

		body, readErr := ioutil.ReadAll(downloadedFile)
		if readErr != nil {
			step.logger.Warnd(
				map[string]interface{}{
					"error":      err.Error(),
					"read-error": readErr.Error(),
					"url":        step.model.From,
					"info":       info.Size(),
				},
				"downloader.extracted-read-failed",
			)
			return "", err
		}

		step.logger.Warnd(
			map[string]interface{}{
				"error": err.Error(),
				"url":   step.model.From,
				"info":  info.Size(),
				"body":  string(body),
			},
			"downloader.extract-failed",
		)

		return "", err
	}
	return extractionDir, nil
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
