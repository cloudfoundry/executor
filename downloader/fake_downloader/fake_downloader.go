package fake_downloader

import (
	"errors"
	"net/url"
	"os"
)

type FakeDownloader struct {
	DownloadedUrls []*url.URL
	DownloadSize   int64
	alwaysFail     bool
}

func New() *FakeDownloader {
	return &FakeDownloader{}
}

func (downloader *FakeDownloader) Download(url *url.URL, destinationFile *os.File) (int64, error) {
	if downloader.alwaysFail {
		return 0, errors.New("I accidentally the download")
	}
	downloader.DownloadedUrls = append(downloader.DownloadedUrls, url)
	return downloader.DownloadSize, nil
}

func (downloader *FakeDownloader) AlwaysFail() {
	downloader.alwaysFail = true
}
