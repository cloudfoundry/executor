package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

type Downloader interface {
	Download(url *url.URL, destinationFile *os.File) error
}

type URLDownloader struct{}

func New() Downloader {
	return &URLDownloader{}
}

func (downloader *URLDownloader) Download(url *url.URL, destinationFile *os.File) error {
	resp, err := http.Get(url.String())
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
