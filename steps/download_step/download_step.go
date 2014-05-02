package download_step

import (
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
)

type DownloadStep struct {
	container        warden.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	extractor        extractor.Extractor
	tempDir          string
	logger           *steno.Logger
}

func New(
	container warden.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	extractor extractor.Extractor,
	tempDir string,
	logger *steno.Logger,
) *DownloadStep {
	return &DownloadStep{
		container:        container,
		model:            model,
		cachedDownloader: cachedDownloader,
		extractor:        extractor,
		tempDir:          tempDir,
		logger:           logger,
	}
}

func (step *DownloadStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.container.Handle(),
		},
		"task.handle.download-action",
	)

	//Stream this to the extractor + container when we have streaming support!
	downloadedPath, err := step.download()
	if err != nil {
		return err
	}

	defer func() {
		os.Remove(downloadedPath)
	}()

	if step.model.Extract {
		extractionDir, err := step.extract(downloadedPath)
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
		err := step.container.CopyIn(downloadedPath, step.model.To)
		if err != nil {
			return emittable_error.New(err, "Copying into the container failed")
		}

		return err
	}
}

func (step *DownloadStep) download() (string, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		return "", err
	}

	tempFile, err := ioutil.TempFile(step.tempDir, "downloaded")
	defer tempFile.Close()
	if err != nil {
		return "", err
	}

	downloadedFile, err := step.cachedDownloader.Fetch(url, step.model.Cache)
	defer downloadedFile.Close()
	if err != nil {
		return "", err
	}

	io.Copy(tempFile, downloadedFile)

	return tempFile.Name(), nil
}

func (step *DownloadStep) extract(downloadedPath string) (string, error) {
	extractionDir, err := ioutil.TempDir(step.tempDir, "extracted")
	if err != nil {
		return "", err
	}

	err = step.extractor.Extract(downloadedPath, extractionDir)
	if err != nil {
		step.logger.Warnd(
			map[string]interface{}{
				"error": err.Error(),
				"url":   step.model.From,
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
