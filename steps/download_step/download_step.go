package download_step

import (
	"archive/tar"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/archiver/compressor"
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
		step.logger.Errord(
			map[string]interface{}{
				"handle": step.container.Handle(),
				"from":   step.model.From,
				"to":     step.model.To,
				"error":  err,
			},
			"task.handle.download-failed",
		)

		return err
	}

	defer os.Remove(downloadedPath)

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
		// TODO - get rid of this file
		streamIn, err := step.container.StreamIn(filepath.Dir(step.model.To))
		if err != nil {
			return err
		}

		downloadedFile, err := os.Open(downloadedPath)
		if err != nil {
			return err
		}

		defer downloadedFile.Close()

		err = writeTarTo(filepath.Base(step.model.To), downloadedFile, streamIn)
		if err != nil {
			return emittable_error.New(err, "Copying into the container failed")
		}

		return nil
	}
}

func (step *DownloadStep) download() (string, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		return "", err
	}

	tempFile, err := ioutil.TempFile(step.tempDir, "downloaded")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	downloadedFile, err := step.cachedDownloader.Fetch(url, step.model.CacheKey)
	if err != nil {
		return "", err
	}
	defer downloadedFile.Close()

	_, err = io.Copy(tempFile, downloadedFile)
	if err != nil {
		return "", err
	}

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
	streamIn, err := step.container.StreamIn(destination)
	if err != nil {
		return err
	}

	err = compressor.WriteTar(source+string(filepath.Separator), streamIn)
	if err != nil {
		return err
	}

	return streamIn.Close()
}

func writeTarTo(name string, source *os.File, destination io.WriteCloser) error {
	tarWriter := tar.NewWriter(destination)

	fileInfo, err := source.Stat()
	if err != nil {
		return err
	}

	err = tarWriter.WriteHeader(&tar.Header{
		Name:       name,
		Size:       fileInfo.Size(),
		Mode:       0644,
		AccessTime: time.Now(),
		ChangeTime: time.Now(),
	})
	if err != nil {
		return err
	}

	_, err = io.Copy(tarWriter, source)
	if err != nil {
		return err
	}

	if err := tarWriter.Flush(); err != nil {
		return err
	}

	if err := destination.Close(); err != nil {
		return err
	}

	return nil
}
