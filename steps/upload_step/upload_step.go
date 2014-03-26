package upload_step

import (
	"io/ioutil"
	"net/url"
	"os"
	"os/user"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type UploadStep struct {
	containerHandle string
	model           models.UploadAction
	uploader        uploader.Uploader
	tempDir         string
	backendPlugin   backend_plugin.BackendPlugin
	wardenClient    gordon.Client
	logger          *steno.Logger
}

func New(
	containerHandle string,
	model models.UploadAction,
	uploader uploader.Uploader,
	tempDir string,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *UploadStep {
	return &UploadStep{
		containerHandle: containerHandle,
		model:           model,
		uploader:        uploader,
		tempDir:         tempDir,
		wardenClient:    wardenClient,
		logger:          logger,
	}
}

func (step *UploadStep) Perform() error {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.containerHandle,
		},
		"runonce.handle.upload-step",
	)

	tempFile, err := ioutil.TempFile(step.tempDir, "upload")
	if err != nil {
		return err
	}
	fileLocation := tempFile.Name()
	tempFile.Close()
	defer os.RemoveAll(fileLocation)

	currentUser, err := user.Current()
	if err != nil {
		panic("existential failure: " + err.Error())
	}

	_, err = step.wardenClient.CopyOut(
		step.containerHandle,
		step.model.From,
		fileLocation,
		currentUser.Username,
	)
	if err != nil {
		return err
	}

	url, err := url.Parse(step.model.To)
	if err != nil {
		return err
	}

	return step.uploader.Upload(fileLocation, url)
}

func (step *UploadStep) Cancel() {}

func (step *UploadStep) Cleanup() {}
