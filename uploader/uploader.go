package uploader

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	steno "github.com/cloudfoundry/gosteno"
)

type Uploader interface {
	Upload(fileLocation string, destinationUrl *url.URL) (int64, error)
}

type URLUploader struct {
	timeout time.Duration
	logger  *steno.Logger
}

func New(timeout time.Duration, logger *steno.Logger) Uploader {
	return &URLUploader{
		timeout: timeout,
		logger:  logger,
	}
}

func (uploader *URLUploader) Upload(fileLocation string, url *url.URL) (int64, error) {
	httpTransport := &http.Transport{
		ResponseHeaderTimeout: uploader.timeout,
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}

	var resp *http.Response
	var err error
	var uploadedBytes int64

	for attempt := 0; attempt < 3; attempt++ {
		var request *http.Request
		var sourceFile *os.File
		var fileInfo os.FileInfo

		uploader.logger.Infof("uploader.attempt #%d", attempt)

		sourceFile, err = os.Open(fileLocation)
		if err != nil {
			return 0, err
		}

		fileInfo, err = sourceFile.Stat()
		if err != nil {
			return 0, err
		}

		request, err = http.NewRequest("POST", url.String(), sourceFile)
		uploadedBytes = fileInfo.Size()
		request.ContentLength = uploadedBytes
		request.Header.Set("Content-Type", "application/octet-stream")

		resp, err = httpClient.Do(request)
		if err == nil {
			defer resp.Body.Close()
			break
		}
	}

	if err != nil {
		return 0, err
	}

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("Upload failed: Status code %d", resp.StatusCode)
	}

	return int64(uploadedBytes), nil
}
