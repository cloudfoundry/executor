package uploader

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
)

type Uploader interface {
	Upload(sourceFile *os.File, destinationUrl *url.URL) error
}

type URLUploader struct{}

func New() Uploader {
	return &URLUploader{}
}

func (uploader *URLUploader) Upload(sourceFile *os.File, url *url.URL) error {
	resp, err := http.Post(url.String(), "application/octet-stream", sourceFile)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Download failed: Status code %d", resp.StatusCode)
	}

	return nil
}
