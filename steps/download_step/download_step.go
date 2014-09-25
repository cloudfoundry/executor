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
	logger = logger.Session("DownloadAction", lager.Data{
		"from": model.From,
		"to":   model.To,
	})
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
	step.logger.Info("starting")

	//Stream this to the extractor + container when we have streaming support!
	downloadedPath, err := step.download()
	if err != nil {
		return err
	}

	defer os.Remove(downloadedPath)

	if step.model.Extract {
		return step.extract(downloadedPath)
	}

	return step.streamToContainer(downloadedPath)
}

func (step *DownloadStep) download() (string, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		step.logger.Error("parse-request-uri-error", err)
		return "", err
	}

	tempFile, err := ioutil.TempFile(step.tempDir, "downloaded")
	if err != nil {
		step.logger.Error("tempfile-create-error", err)
		return "", err
	}
	defer tempFile.Close()

	downloadedFile, err := step.cachedDownloader.Fetch(url, step.model.CacheKey)
	if err != nil {
		step.logger.Error("cached-downloader-fetch-error", err)
		return "", err
	}
	defer downloadedFile.Close()

	_, err = io.Copy(tempFile, downloadedFile)
	if err != nil {
		step.logger.Error("tempfile-copy-failed", err)
		return "", err
	}

	step.logger.Info("download-successful", lager.Data{
		"tempFile": tempFile.Name(),
	})
	return tempFile.Name(), nil
}

func (step *DownloadStep) extract(downloadedPath string) error {
	extractionDir, err := ioutil.TempDir(step.tempDir, "extracted")
	if err != nil {
		step.logger.Error("extract-failed-to-create-tempdir", err, lager.Data{
			"tempDir": step.tempDir,
		})
		return err
	}

	err = step.extractor.Extract(downloadedPath, extractionDir)
	if err != nil {
		step.logger.Error("failed-to-extract", err, lager.Data{
			"extractionDir":  extractionDir,
			"downloadedPath": downloadedPath,
		})
		return emittable_error.New(err, "Extraction failed")
	}

	defer os.RemoveAll(extractionDir)

	err = step.copyExtractedFiles(extractionDir, step.model.To)
	if err != nil {
		return emittable_error.New(err, "Copying into the container failed")
	}

	step.logger.Info("extract-successful")
	return nil
}

func (step *DownloadStep) streamToContainer(downloadedPath string) error {
	reader, writer := io.Pipe()

	go writeTarTo(filepath.Base(step.model.To), downloadedPath, writer)

	err := step.streamIn(filepath.Dir(step.model.To), reader)
	if err != nil {
		return err
	}

	step.logger.Info("stream-to-container-successful")
	return nil
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
			step.logger.Error("wite-tar-failed", err, lager.Data{
				"source":      source,
				"destination": destination,
			})
			writer.CloseWithError(err)
		}
	}()

	return step.streamIn(destination, reader)
}

func (step *DownloadStep) streamIn(destination string, reader io.Reader) error {
	err := step.container.StreamIn(destination, reader)
	if err != nil {
		step.logger.Error("failed-to-stream-in", err, lager.Data{
			"destination": destination,
		})
		return emittable_error.New(err, "Copying into the container failed")
	}

	return err
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
