package fake_downloader

import (
	"errors"
	"io"
	"net/url"
	"os"
)

type FakeDownloader struct {
	DownloadedUrls []*url.URL
	SourceFile     *os.File
	alwaysFail     bool
}

func (downloader *FakeDownloader) Download(url *url.URL, destinationFile *os.File) error {
	if downloader.alwaysFail {
		return errors.New("I accidentally the download")
	}

	downloader.DownloadedUrls = append(downloader.DownloadedUrls, url)

	if downloader.SourceFile != nil {
		downloader.SourceFile.Seek(0, 0)
		destinationFile.Seek(0, 0)
		_, err := io.Copy(destinationFile, downloader.SourceFile)
		if err != nil {
			println(err.Error())
		}

	}

	return nil
}

func (downloader *FakeDownloader) AlwaysFail() {
	downloader.alwaysFail = true
}
