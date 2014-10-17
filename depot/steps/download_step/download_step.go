package download_step

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/executor/depot/steps/emittable_error"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

type DownloadStep struct {
	container        garden_api.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	semaphore        chan struct{}
	logger           lager.Logger
}

func New(
	container garden_api.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	semaphore chan struct{},
	logger lager.Logger,
) *DownloadStep {
	logger = logger.Session("DownloadAction", lager.Data{
		"to":       model.To,
		"cacheKey": model.CacheKey,
	})

	return &DownloadStep{
		container:        container,
		model:            model,
		cachedDownloader: cachedDownloader,
		semaphore:        semaphore,
		logger:           logger,
	}
}

func (step *DownloadStep) Perform() error {
	step.logger.Info("download-waiting")
	step.logger.Info(fmt.Sprintf("download-semaphore: %#v", step.semaphore))

	startWaiting := time.Now()

	step.semaphore <- struct{}{}

	step.logger.Info(fmt.Sprintf("download-wait-time=%v", time.Since(startWaiting).Seconds()))
	startDownloading := time.Now()

	defer func() {
		<-step.semaphore
		step.logger.Info(fmt.Sprintf("download-time=%v", time.Since(startDownloading).Seconds()))
	}()

	step.logger.Info("starting")

	downloadedFile, err := step.download()
	if err != nil {
		return emittable_error.New(err, "Downloading failed")
	}
	defer downloadedFile.Close()

	return step.streamIn(step.model.To, downloadedFile)
}

func (step *DownloadStep) download() (io.ReadCloser, error) {
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		step.logger.Error("parse-request-uri-error", err)
		return nil, err
	}

	return step.cachedDownloader.Fetch(url, step.model.CacheKey, cacheddownloader.TarTransform)
}

func (step *DownloadStep) Cancel() {}

func (step *DownloadStep) Cleanup() {}

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
