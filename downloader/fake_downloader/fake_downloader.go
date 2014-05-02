package fake_downloader

import (
	"errors"
	"net/url"
	"os"
	"time"
)

type FakeDownloader struct {
	DownloadedUrls  []*url.URL
	DownloadContent []byte
	alwaysFail      bool

	ModifiedSinceTime time.Time
	IsModified        bool
}

func (downloader *FakeDownloader) Download(url *url.URL, destinationFile *os.File, ifModifiedSince time.Time) (bool, int64, error) {
	if downloader.alwaysFail {
		return false, 0, errors.New("I accidentally the download")
	}
	downloader.DownloadedUrls = append(downloader.DownloadedUrls, url)
	downloader.ModifiedSinceTime = ifModifiedSince

	if ifModifiedSince.IsZero() || downloader.IsModified {
		destinationFile.Write(downloader.DownloadContent)
		return true, int64(len(downloader.DownloadContent)), nil
	}

	return false, 0, nil
}

func (downloader *FakeDownloader) AlwaysFail() {
	downloader.alwaysFail = true
}
