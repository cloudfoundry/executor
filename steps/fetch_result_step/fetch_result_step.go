package fetch_result_step

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type FetchResultStep struct {
	container         warden.Container
	fetchResultAction models.FetchResultAction
	tempDir           string
	logger            *steno.Logger
	result            *string
}

func New(
	container warden.Container,
	fetchResultAction models.FetchResultAction,
	tempDir string,
	logger *steno.Logger,
	result *string,
) *FetchResultStep {
	return &FetchResultStep{
		container:         container,
		fetchResultAction: fetchResultAction,
		tempDir:           tempDir,
		logger:            logger,
		result:            result,
	}
}

func (step *FetchResultStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.container.Handle(),
		},
		"task.handle.fetch-result-step",
	)

	data, err := step.copyAndReadResult()
	if err != nil {
		return emittable_error.New(err, "Copying out of the container failed")
	}

	*step.result = string(data)

	return nil
}

func (step *FetchResultStep) copyAndReadResult() ([]byte, error) {
	tempFile, err := ioutil.TempFile(step.tempDir, "fetch-result")
	if err != nil {
		return nil, err
	}
	fileName := tempFile.Name()
	tempFile.Close()
	defer os.RemoveAll(fileName)

	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}

	err = step.container.CopyOut(
		step.fetchResultAction.File,
		fileName,
		currentUser.Username,
	)

	if err != nil {
		return nil, err
	}

	resultFile, err := os.Open(fileName)
	defer resultFile.Close()
	if err != nil {
		return nil, err
	}

	fileStat, err := resultFile.Stat()
	if err != nil {
		return nil, err
	}

	if fileStat.Size() > 1024*10 {
		return nil, fmt.Errorf("result file size exceeds allowed limit (got %d bytes > 10 kilo-bytes)", fileStat.Size())
	}

	return ioutil.ReadAll(resultFile)
}

func (step *FetchResultStep) Cancel() {}

func (step *FetchResultStep) Cleanup() {}
