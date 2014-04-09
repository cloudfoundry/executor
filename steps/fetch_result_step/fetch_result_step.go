package fetch_result_step

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry-incubator/gordon"
)

type FetchResultStep struct {
	handle            string
	fetchResultAction models.FetchResultAction
	tempDir           string
	wardenClient      gordon.Client
	logger            *steno.Logger
	result            *string
}

func New(
	handle string,
	fetchResultAction models.FetchResultAction,
	tempDir string,
	wardenClient gordon.Client,
	logger *steno.Logger,
	result *string,
) *FetchResultStep {
	return &FetchResultStep{
		handle:            handle,
		fetchResultAction: fetchResultAction,
		tempDir:           tempDir,
		wardenClient:      wardenClient,
		logger:            logger,
		result:            result,
	}
}

func (step *FetchResultStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.handle,
		},
		"runonce.handle.fetch-result-step",
	)

	tempFile, err := ioutil.TempFile(step.tempDir, "fetch-result")
	if err != nil {
		return err
	}
	fileName := tempFile.Name()
	tempFile.Close()
	defer os.RemoveAll(fileName)

	currentUser, err := user.Current()
	if err != nil {
		panic("existential failure: " + err.Error())
	}

	_, err = step.wardenClient.CopyOut(
		step.handle,
		step.fetchResultAction.File,
		fileName,
		currentUser.Username)
	if err != nil {
		return err
	}

	resultFile, err := os.Open(fileName)
	defer resultFile.Close()
	if err != nil {
		return err
	}

	fileStat, err := resultFile.Stat()
	if err != nil {
		return err
	}

	if fileStat.Size() > 1024*10 {
		return fmt.Errorf("result file size exceeds allowed limit (got %d bytes > 10 kilo-bytes)", fileStat.Size())
	}

	data, err := ioutil.ReadAll(resultFile)
	if err != nil {
		return err
	}

	*step.result = string(data)

	return nil
}

func (step *FetchResultStep) Cancel() {}

func (step *FetchResultStep) Cleanup() {}
