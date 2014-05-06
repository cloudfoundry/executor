package fetch_result_step

import (
	"archive/tar"
	"fmt"

	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

const MAX_RESULT_SIZE = 1024 * 10

var ErrResultTooLarge = fmt.Errorf("result file size exceeds limit of %d bytes", MAX_RESULT_SIZE)

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
	reader, err := step.container.StreamOut(step.fetchResultAction.File)
	if err != nil {
		return nil, err
	}

	tarReader := tar.NewReader(reader)

	_, err = tarReader.Next()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, MAX_RESULT_SIZE+1)

	n, err := tarReader.Read(buf)
	if n > MAX_RESULT_SIZE {
		return nil, ErrResultTooLarge
	}

	return buf[:n], nil
}

func (step *FetchResultStep) Cancel() {}

func (step *FetchResultStep) Cleanup() {}
