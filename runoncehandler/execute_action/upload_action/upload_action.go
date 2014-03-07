package upload_action

import (
	"io/ioutil"
	"net/url"
	"os"
	"os/user"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type UploadAction struct {
	runOnce       *models.RunOnce
	model         models.UploadAction
	uploader      uploader.Uploader
	tempDir       string
	backendPlugin backend_plugin.BackendPlugin
	wardenClient  gordon.Client
	logger        *steno.Logger
}

func New(
	runOnce *models.RunOnce,
	model models.UploadAction,
	uploader uploader.Uploader,
	tempDir string,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *UploadAction {
	return &UploadAction{
		runOnce:      runOnce,
		model:        model,
		uploader:     uploader,
		tempDir:      tempDir,
		wardenClient: wardenClient,
		logger:       logger,
	}
}

func (action *UploadAction) Perform(result chan<- error) {
	action.logger.Infod(
		map[string]interface{}{
			"handle": action.runOnce.ContainerHandle,
		},
		"runonce.handle.upload-action",
	)

	result <- action.perform()
}

func (action *UploadAction) Cancel() {}

func (action *UploadAction) Cleanup() {}

func (action *UploadAction) perform() error {
	tempFile, err := ioutil.TempFile(action.tempDir, "upload")
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
		action.runOnce.ContainerHandle,
		action.model.From,
		fileName,
		currentUser.Username,
	)
	if err != nil {
		return err
	}

	fileToUpload, err := os.Open(fileName)
	if err != nil {
		return err
	}

	url, err := url.Parse(action.model.To)
	if err != nil {
		return err
	}

	return action.uploader.Upload(fileToUpload, url)
}
