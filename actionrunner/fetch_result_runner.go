package actionrunner

import (
	"fmt"
	"github.com/vito/gordon"
	"io/ioutil"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FetchResultRunner struct {
	wardenClient gordon.Client
	tempDir      string
}

func NewFetchResultRunner(wardenClient gordon.Client, tempDir string) *FetchResultRunner {
	return &FetchResultRunner{
		wardenClient: wardenClient,
		tempDir:      tempDir,
	}
}

func (fetchResultRunner *FetchResultRunner) perform(containerHandle string, action models.FetchResultAction) (string, error) {
	tempFile, err := ioutil.TempFile(fetchResultRunner.tempDir, "fetch-result")
	if err != nil {
		return "", err
	}
	fileName := tempFile.Name()
	tempFile.Close()
	defer os.RemoveAll(fileName)

	currentUser, err := user.Current()
	if err != nil {
		panic("existential failure: " + err.Error())
	}

	_, err = fetchResultRunner.wardenClient.CopyOut(containerHandle, action.File, fileName, currentUser.Username)
	if err != nil {
		return "", err
	}

	resultFile, err := os.Open(fileName)
	defer resultFile.Close()
	if err != nil {
		return "", err
	}

	fileStat, err := resultFile.Stat()
	if err != nil {
		return "", err
	}

	if fileStat.Size() > 1024*10 {
		return "", fmt.Errorf("result file size exceeds allowed limit (got %d bytes > 10 kilo-bytes)", fileStat.Size())
	}

	data, err := ioutil.ReadAll(resultFile)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
