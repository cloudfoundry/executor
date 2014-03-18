package fetch_result_action

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type FetchResultAction struct {
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
) *FetchResultAction {
	return &FetchResultAction{
		handle:            handle,
		fetchResultAction: fetchResultAction,
		tempDir:           tempDir,
		wardenClient:      wardenClient,
		logger:            logger,
		result:            result,
	}
}

func (action *FetchResultAction) Perform() error {
	action.logger.Infod(
		map[string]interface{}{
			"handle": action.handle,
		},
		"runonce.handle.fetch-result-action",
	)

	tempFile, err := ioutil.TempFile(action.tempDir, "fetch-result")
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

	_, err = action.wardenClient.CopyOut(
		action.handle,
		action.fetchResultAction.File,
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

	*action.result = string(data)

	return nil
}

func (action *FetchResultAction) Cancel() {}

func (action *FetchResultAction) Cleanup() {}
