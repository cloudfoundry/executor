package steps

import (
	"fmt"
	"io"
	"net/url"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/bytefmt"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

type downloadStep struct {
	container        garden_api.Container
	model            models.DownloadAction
	cachedDownloader cacheddownloader.CachedDownloader
	streamer         log_streamer.LogStreamer
	rateLimiter      chan struct{}
	cancelChan       chan struct{}
	logger           lager.Logger
}

func NewDownload(
	container garden_api.Container,
	model models.DownloadAction,
	cachedDownloader cacheddownloader.CachedDownloader,
	rateLimiter chan struct{},
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
) *downloadStep {
	logger = logger.Session("download-step", lager.Data{
		"to":       model.To,
		"cacheKey": model.CacheKey,
	})

	cancelChan := make(chan struct{})

	return &downloadStep{
		container:        container,
		model:            model,
		cachedDownloader: cachedDownloader,
		streamer:         streamer,
		rateLimiter:      rateLimiter,
		cancelChan:       cancelChan,
		logger:           logger,
	}
}

func (step *downloadStep) Cancel() {
	close(step.cancelChan)
}

func (step *downloadStep) Perform() error {
	select {
	case step.rateLimiter <- struct{}{}:
	case <-step.cancelChan:
		return CancelError{}
	}
	defer func() {
		<-step.rateLimiter
	}()

	err := step.perform()
	if err != nil {
		select {
		case <-step.cancelChan:
			return CancelError{}
		default:
			return err
		}
	}

	return nil
}

func (step *downloadStep) perform() error {
	step.emit("Downloading %s...\n", step.model.Artifact)

	downloadedFile, err := step.fetch()
	if err != nil {
		return NewEmittableError(err, "Downloading failed")
	}
	defer downloadedFile.Close()

	err = step.streamIn(step.model.To, downloadedFile)
	if err != nil {
		return NewEmittableError(err, "Copying into the container failed")
	}

	step.emit("Downloaded %s (%s)\n", step.model.Artifact, downloadSize(downloadedFile))
	return nil
}

func (step *downloadStep) fetch() (io.ReadCloser, error) {
	step.logger.Info("fetch-starting")
	url, err := url.ParseRequestURI(step.model.From)
	if err != nil {
		step.logger.Error("parse-request-uri-error", err)
		return nil, err
	}

	tarStream, err := step.cachedDownloader.Fetch(url, step.model.CacheKey, cacheddownloader.TarTransform, step.cancelChan)
	if err != nil {
		step.logger.Error("fetch-failed", err)
		return nil, err
	}

	step.logger.Info("fetch-complete")
	return tarStream, nil
}

func (step *downloadStep) streamIn(destination string, reader io.Reader) error {
	step.logger.Info("stream-in-starting")
	err := step.container.StreamIn(destination, reader)
	if err != nil {
		step.logger.Error("stream-in-failed", err, lager.Data{
			"destination": destination,
		})
		return err
	}

	step.logger.Info("stream-in-complete")
	return nil
}

func (step *downloadStep) emit(format string, a ...interface{}) {
	if step.model.Artifact != "" {
		fmt.Fprintf(step.streamer.Stdout(), format, a...)
	}
}

func downloadSize(downloadedFile interface{}) string {
	if f, ok := downloadedFile.(*cacheddownloader.CachedFile); ok {
		fi, err := f.Stat()
		if err != nil {
			return "unknown"
		}
		return bytefmt.ByteSize(uint64(fi.Size()))
	}

	return "unknown"
}
