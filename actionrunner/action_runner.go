package actionrunner

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
)

type ActionRunnerInterface interface {
	Run(containerHandle string, actions []models.ExecutorAction) error
}

type BackendPlugin interface {
	BuildRunScript(models.RunAction) string
}

type ActionRunner struct {
	wardenClient  gordon.Client
	backendPlugin BackendPlugin
	downloader    downloader.Downloader
}

type RunActionTimeoutError struct {
	Action models.RunAction
}

func (e RunActionTimeoutError) Error() string {
	return fmt.Sprintf("action timed out after %s", e.Action.Timeout)
}

func New(wardenClient gordon.Client, backendPlugin BackendPlugin, downloader downloader.Downloader) *ActionRunner {
	return &ActionRunner{
		wardenClient:  wardenClient,
		backendPlugin: backendPlugin,
		downloader:    downloader,
	}
}

func (runner *ActionRunner) Run(containerHandle string, actions []models.ExecutorAction) error {
	for _, action := range actions {
		var err error
		switch a := action.Action.(type) {
		case models.RunAction:
			err = runner.performRunAction(containerHandle, a)
		case models.DownloadAction:
			err = runner.performDownloadAction(containerHandle, a)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (runner *ActionRunner) performRunAction(containerHandle string, action models.RunAction) error {
	exitStatusChan := make(chan uint32, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if action.Timeout != 0 {
		timeoutChan = time.After(action.Timeout)
	}

	go func() {
		_, stream, err := runner.wardenClient.Run(
			containerHandle,
			runner.backendPlugin.BuildRunScript(action),
		)

		if err != nil {
			errChan <- err
			return
		}

		for payload := range stream {
			if payload.ExitStatus != nil {
				exitStatusChan <- payload.GetExitStatus()
			}
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		if exitStatus != 0 {
			return fmt.Errorf("Process returned with exit value: %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return RunActionTimeoutError{Action: action}
	}

	panic("unreachable")
}

func (runner *ActionRunner) performDownloadAction(containerHandle string, action models.DownloadAction) error {
	url, err := url.Parse(action.From)
	if err != nil {
		return err
	}

	downloadedFile, err := ioutil.TempFile(os.TempDir(), "test-downloaded")
	if err != nil {
		return err
	}
	defer func() {
		downloadedFile.Close()
		os.RemoveAll(downloadedFile.Name())
	}()

	err = runner.downloader.Download(url, downloadedFile)
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

		return filepath.Walk(extractedFilesLocation, func(path string, info os.FileInfo, err error) error {
			relativePath, err := filepath.Rel(extractedFilesLocation, path)
			if err != nil {
				return err
			}
			wardenPath := filepath.Join(action.To, relativePath)
			_, err = runner.wardenClient.CopyIn(containerHandle, path, wardenPath)
			return err
		})
	} else {
		_, err = runner.wardenClient.CopyIn(containerHandle, downloadedFile.Name(), action.To)
		if err != nil {
			return err
		}
	}

	return nil
}
