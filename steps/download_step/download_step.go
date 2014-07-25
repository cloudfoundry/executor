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
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

type DownloadStep struct {
	container        warden.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	extractor        extractor.Extractor
	tempDir          string
	logger           lager.Logger
}

func New(
	container warden.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	extractor extractor.Extractor,
	tempDir string,
	logger lager.Logger,
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
	step.logger.Info("download")

	//Stream this to the extractor + container when we have streaming support!
	downloadedPath, err := step.download()
	if err != nil {
		step.logger.Error("failed-to-download", err, lager.Data{
			"from": step.model.From,
			"to":   step.model.To,
		})

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
		reader, writer := io.Pipe()

		go writeTarTo(filepath.Base(step.model.To), downloadedPath, writer)

		err = step.container.StreamIn(filepath.Dir(step.model.To), reader)
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
		step.logger.Error("failed-to-extract", err, lager.Data{
			"url": step.model.From,
		})

		return "", err
	}

	return extractionDir, nil
}

func (step *DownloadStep) Cancel() {}

func (step *DownloadStep) Cleanup() {}

func (step *DownloadStep) copyExtractedFiles(source string, destination string) error {
	reader, writer := io.Pipe()

	go func() {
		err := compressor.WriteTar(source+string(filepath.Separator), writer)
		if err == nil {
			writer.Close()
		} else {
			writer.CloseWithError(err)
		}
	}()

	return step.container.StreamIn(destination, reader)
}

func writeTarTo(name string, sourcePath string, destination *io.PipeWriter) {
	source, err := os.Open(sourcePath)
	if err != nil {
		destination.CloseWithError(err)
		return
	}
	defer source.Close()

	tarWriter := tar.NewWriter(destination)

	fileInfo, err := source.Stat()
	if err != nil {
		destination.CloseWithError(err)
		return
	}

	err = tarWriter.WriteHeader(&tar.Header{
		Name:       name,
		Size:       fileInfo.Size(),
		Mode:       0644,
		AccessTime: time.Now(),
		ChangeTime: time.Now(),
	})
	if err != nil {
		destination.CloseWithError(err)
		return
	}

	_, err = io.Copy(tarWriter, source)
	if err != nil {
		destination.CloseWithError(err)
		return
	}

	if err := tarWriter.Flush(); err != nil {
		destination.CloseWithError(err)
		return
	}

	destination.Close()
}
