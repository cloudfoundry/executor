package steps

import (
	"io"
	"net/url"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

type DownloadStep struct {
	container        garden_api.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	rateLimiter      chan struct{}
	logger           lager.Logger
}

func NewDownload(
	container garden_api.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	rateLimiter chan struct{},
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
		rateLimiter:      rateLimiter,
		logger:           logger,
	}
}

func (step *DownloadStep) Perform() error {
	step.rateLimiter <- struct{}{}
	defer func() {
		<-step.rateLimiter
	}()

	step.logger.Info("starting")

	downloadedFile, err := step.download()
	if err != nil {
		return NewEmittableError(err, "Downloading failed")
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

func (step *DownloadStep) streamIn(destination string, reader io.Reader) error {
	err := step.container.StreamIn(destination, reader)
	if err != nil {
		step.logger.Error("failed-to-stream-in", err, lager.Data{
			"destination": destination,
		})
		return NewEmittableError(err, "Copying into the container failed")
	}

	return err
}
