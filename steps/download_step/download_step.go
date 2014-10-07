package download_step

import (
	"archive/tar"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

type DownloadStep struct {
	container        garden_api.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	extractor        extractor.Extractor
	tempDir          string
	logger           lager.Logger
}

func New(
	container garden_api.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	extractor extractor.Extractor,
	tempDir string,
	logger lager.Logger,
) *DownloadStep {
	logger = logger.Session("DownloadAction", lager.Data{
		"from":     model.From,
		"to":       model.To,
		"cacheKey": model.CacheKey,
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
	downloadedFile, err := step.download()
	if err != nil {
		return err
	}

	defer downloadedFile.Close()

	if step.model.Extract {
		return step.copyExtractedFiles(downloadedFile.File, step.model.To)
	}

	return step.streamToContainer(downloadedFile)
}

func (step *DownloadStep) download() (*cacheddownloader.CachedFile, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		step.logger.Error("parse-request-uri-error", err)
		return nil, err
	}

	downloadedFile, err := step.cachedDownloader.Fetch(url, step.model.CacheKey)
	if err != nil {
		step.logger.Error("cached-downloader-fetch-error", err)
		return nil, err
	}

	step.logger.Info("fetch-successful", lager.Data{
		"cache-key": step.model.CacheKey,
	})

	return downloadedFile, nil
}

func (step *DownloadStep) streamToContainer(downloadedFile *cacheddownloader.CachedFile) error {
	reader, writer := io.Pipe()

	go writeTarTo(filepath.Base(step.model.To), downloadedFile, writer)

	err := step.streamIn(filepath.Dir(step.model.To), reader)
	if err != nil {
		return err
	}

	step.logger.Info("stream-to-container-successful")
	return nil
}

func (step *DownloadStep) Cancel() {}

func (step *DownloadStep) Cleanup() {}

func (step *DownloadStep) copyExtractedFiles(source *os.File, destination string) error {
	reader, writer := io.Pipe()

	extractErrCh := make(chan error, 1)

	go func() {
		step.logger.Info("extracting", lager.Data{
			"source": source.Name(),
		})

		extractErrCh <- step.extractor.Extract(source, writer)

		writer.Close()

		step.logger.Info("extracted", lager.Data{
			"source": source.Name(),
		})
	}()

	step.logger.Info("streaming-in", lager.Data{
		"source": source.Name(),
	})

	streamErr := step.streamIn(destination, reader)

	step.logger.Info("streamed-in", lager.Data{
		"source": source.Name(),
	})

	extractErr := <-extractErrCh
	if extractErr != nil {
		step.logger.Error("failed-to-extract", extractErr, lager.Data{
			"source":      source,
			"destination": destination,
		})

		return emittable_error.New(extractErr, "Extraction failed")
	} else if streamErr != nil {
		step.logger.Error("failed-to-stream", streamErr, lager.Data{
			"source":      source,
			"destination": destination,
		})

		return emittable_error.New(extractErr, "Streaming in failed")
	}

	return nil
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

func writeTarTo(name string, sourceFile *cacheddownloader.CachedFile, destination *io.PipeWriter) {
	tarWriter := tar.NewWriter(destination)

	fileInfo, err := sourceFile.Stat()
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

	_, err = io.Copy(tarWriter, sourceFile)
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
