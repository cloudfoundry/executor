package actionrunner

import (
	"io/ioutil"
	"net/url"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
)

type UploadRunner struct {
	uploader     uploader.Uploader
	wardenClient gordon.Client
	tempDir      string
}

func NewUploadRunner(uploader uploader.Uploader, wardenClient gordon.Client, tempDir string) *UploadRunner {
	return &UploadRunner{
		uploader:     uploader,
		wardenClient: wardenClient,
		tempDir:      tempDir,
	}
}

func (uploadRunner *UploadRunner) perform(containerHandle string, action models.UploadAction) error {
	fileToUpload, err := ioutil.TempFile(uploadRunner.tempDir, "upload")
	if err != nil {
		return err
	}
	defer os.RemoveAll(fileToUpload.Name())

	currentUser, err := user.Current()
	if err != nil {
		panic("existential failure: " + err.Error())
	}

	_, err = uploadRunner.wardenClient.CopyOut(containerHandle, action.From, fileToUpload.Name(), currentUser.Username)
	if err != nil {
		return err
	}

	url, err := url.Parse(action.To)
	if err != nil {
		return err
	}

	return uploadRunner.uploader.Upload(fileToUpload, url)
}
