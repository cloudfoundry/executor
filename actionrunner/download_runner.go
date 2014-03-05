package actionrunner

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
)

type DownloadRunner struct {
	downloader    downloader.Downloader
	wardenClient  gordon.Client
	tempDir       string
	backendPlugin backend_plugin.BackendPlugin
}

func NewDownloadRunner(downloader downloader.Downloader, wardenClient gordon.Client, tempDir string, backendPlugin backend_plugin.BackendPlugin) *DownloadRunner {
	return &DownloadRunner{
		downloader:    downloader,
		wardenClient:  wardenClient,
		tempDir:       tempDir,
		backendPlugin: backendPlugin,
	}
}

func (downloadRunner *DownloadRunner) perform(containerHandle string, action models.DownloadAction) error {
	url, err := url.Parse(action.From)
	if err != nil {
		return err
	}

	downloadedFile, err := ioutil.TempFile(downloadRunner.tempDir, "downloaded")
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

	createParentDirCommand := downloadRunner.backendPlugin.BuildCreateDirectoryRecursivelyCommand(filepath.Dir(action.To))
	_, _, err = downloadRunner.wardenClient.Run(containerHandle, createParentDirCommand)
	if err != nil {
		return err
	}

	if action.Extract {
		extractionDir, err := ioutil.TempDir(downloadRunner.tempDir, "extracted")
		if err != nil {
			return err
		}

		err = extractor.Extract(downloadedFile.Name(), extractionDir)
		defer os.RemoveAll(extractionDir)
		if err != nil {
			return err
		}

		return downloadRunner.copyExtractedFiles(containerHandle, extractionDir, action.To)
	} else {
		_, err = downloadRunner.wardenClient.CopyIn(containerHandle, downloadedFile.Name(), action.To)
		return err
	}
}

func (downloadRunner *DownloadRunner) copyExtractedFiles(containerHandle string, source string, destination string) error {
	_, err := downloadRunner.wardenClient.CopyIn(containerHandle, source+string(filepath.Separator), destination+string(filepath.Separator))
	return err
}
