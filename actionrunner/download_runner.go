package actionrunner

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
)

type DownloadRunner struct {
	downloader   downloader.Downloader
	wardenClient gordon.Client
}

func NewDownloadRunner(downloader downloader.Downloader, wardenClient gordon.Client) *DownloadRunner {
	return &DownloadRunner{
		downloader:   downloader,
		wardenClient: wardenClient,
	}
}

func (downloadRunner *DownloadRunner) Perform(containerHandle string, action models.DownloadAction) error {
	url, err := url.Parse(action.From)
	if err != nil {
		return err
	}

	downloadedFile, err := ioutil.TempFile(os.TempDir(), "downloaded")
	if err != nil {
		return err
	}
	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	err = downloadRunner.downloader.Download(url, downloadedFile)
	if err != nil {
		return err
	}

	if action.Extract {
		extractor := extractor.New()

		extractedFilesLocation, err := extractor.Extract(downloadedFile.Name())
		defer os.RemoveAll(extractedFilesLocation)
		if err != nil {
			return err
		}

		return downloadRunner.copyExtractedFiles(containerHandle, extractedFilesLocation, action.To)
	} else {
		_, err = downloadRunner.wardenClient.CopyIn(containerHandle, downloadedFile.Name(), action.To)
		return err
	}
}

func (downloadRunner *DownloadRunner) copyExtractedFiles(containerHandle string, source string, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		relativePath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		wardenPath := filepath.Join(destination, relativePath)
		_, err = downloadRunner.wardenClient.CopyIn(containerHandle, path, wardenPath)
		return err
	})
}
