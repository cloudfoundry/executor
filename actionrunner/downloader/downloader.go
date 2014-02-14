package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	steno "github.com/cloudfoundry/gosteno"
)

type Downloader interface {
	Download(url *url.URL, destinationFile *os.File) error
}

type URLDownloader struct {
	timeout time.Duration
	logger  *steno.Logger
}

func New(timeout time.Duration, logger *steno.Logger) Downloader {
	return &URLDownloader{
		timeout: timeout,
		logger:  logger,
	}
}

func (downloader *URLDownloader) Download(url *url.URL, destinationFile *os.File) error {
	httpTransport := &http.Transport{
		ResponseHeaderTimeout: downloader.timeout,
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}

	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		downloader.logger.Infof("downloader.attempt #%d", attempt)
		resp, err = httpClient.Get(url.String())
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Download failed: Status code %d", resp.StatusCode)
	}

	_, err = io.Copy(destinationFile, resp.Body)
	return err
}
