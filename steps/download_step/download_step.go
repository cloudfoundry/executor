package download_step

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/bytefmt"
)

type DownloadStep struct {
	container  warden.Container
	model      models.DownloadAction
	downloader downloader.Downloader
	extractor  extractor.Extractor
	tempDir    string
	streamer   log_streamer.LogStreamer
	logger     *steno.Logger
}

func New(
	container warden.Container,
	model models.DownloadAction,
	downloader downloader.Downloader,
	extractor extractor.Extractor,
	tempDir string,
	streamer log_streamer.LogStreamer,
	logger *steno.Logger,
) *DownloadStep {
	return &DownloadStep{
		container:  container,
		model:      model,
		downloader: downloader,
		extractor:  extractor,
		tempDir:    tempDir,
		streamer:   streamer,
		logger:     logger,
	}
}

func (step *DownloadStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.container.Handle(),
		},
		"task.handle.download-action",
	)

	downloadedFile, err := step.download()
	if err != nil {
		return err
	}

	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	if step.model.Extract {
		extractionDir, err := step.extract(downloadedFile)
		if err != nil {
			return emittable_error.New(err, "Extraction failed")
		}

		defer os.RemoveAll(extractionDir)

		err = step.copyExtractedFiles(extractionDir, step.model.To)
		if err != nil {
			return emittable_error.New(err, "Copying into the container failed")
		}

		return err
	} else {
		err := step.container.CopyIn(downloadedFile.Name(), step.model.To)
		if err != nil {
			return emittable_error.New(err, "Copying into the container failed")
		}

		return err
	}
}

func (step *DownloadStep) download() (*os.File, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		return nil, err
	}

	downloadedFile, err := ioutil.TempFile(step.tempDir, "downloaded")
	if err != nil {
		return nil, err
	}

	downloadSize, err := step.downloader.Download(url, downloadedFile)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(step.streamer.Stdout(), "Downloaded (%s)\n", bytefmt.ByteSize(uint64(downloadSize)))

	return downloadedFile, err
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
	return step.container.CopyIn(
		source+string(filepath.Separator),
		destination+string(filepath.Separator),
	)
}
