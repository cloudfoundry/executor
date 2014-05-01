package fake_downloader

import (
	"errors"
	"net/url"
	"os"
	"time"
)

type FakeDownloader struct {
	DownloadedUrls        []*url.URL
	DownloadContent       []byte
	DownloadSleepDuration time.Duration
	alwaysFail            bool

	ModifiedSinceURL   *url.URL
	ModifiedSinceTime  time.Time
	IsModified         bool
	ModifiedSinceError error
}

func (downloader *FakeDownloader) Download(url *url.URL, destinationFile *os.File) (int64, error) {
	if downloader.DownloadSleepDuration != 0 {
		time.Sleep(downloader.DownloadSleepDuration)
	}

	if downloader.alwaysFail {
		return 0, errors.New("I accidentally the download")
	}
	downloader.DownloadedUrls = append(downloader.DownloadedUrls, url)
	destinationFile.Write(downloader.DownloadContent)
	return int64(len(downloader.DownloadContent)), nil
}

func (downloader *FakeDownloader) AlwaysFail() {
	downloader.alwaysFail = true
}

func (downloader *FakeDownloader) ModifiedSince(url *url.URL, ifModifiedSince time.Time) (bool, error) {
	downloader.ModifiedSinceURL = url
	downloader.ModifiedSinceTime = ifModifiedSince

	return downloader.IsModified, downloader.ModifiedSinceError
}
