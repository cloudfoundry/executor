package uploader

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type Uploader interface {
	Upload(sourceFile *os.File, destinationUrl *url.URL) error
}

type URLUploader struct {
	timeout time.Duration
}

func New(timeout time.Duration) Uploader {
	return &URLUploader{
		timeout: timeout,
	}
}

func (uploader *URLUploader) Upload(sourceFile *os.File, url *url.URL) error {
	httpTransport := &http.Transport{
		ResponseHeaderTimeout: uploader.timeout,
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}

	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = httpClient.Post(url.String(), "application/octet-stream", sourceFile)
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

	return nil
}
