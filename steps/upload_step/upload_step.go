package upload_step

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/user"

	"github.com/cloudfoundry-incubator/gordon"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/pivotal-golang/bytefmt"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/compressor"
)

type UploadStep struct {
	containerHandle string
	model           models.UploadAction
	uploader        uploader.Uploader
	compressor      compressor.Compressor
	tempDir         string
	wardenClient    gordon.Client
	streamer        log_streamer.LogStreamer
	logger          *steno.Logger
}

func New(
	containerHandle string,
	model models.UploadAction,
	uploader uploader.Uploader,
	compressor compressor.Compressor,
	tempDir string,
	wardenClient gordon.Client,
	streamer log_streamer.LogStreamer,
	logger *steno.Logger,
) *UploadStep {
	return &UploadStep{
		containerHandle: containerHandle,
		model:           model,
		uploader:        uploader,
		compressor:      compressor,
		tempDir:         tempDir,
		wardenClient:    wardenClient,
		streamer:        streamer,
		logger:          logger,
	}
}

func (step *UploadStep) Perform() (err error) {
	step.logger.Infod(
		map[string]interface{}{
			"handle": step.containerHandle,
		},
		"runonce.handle.upload-step",
	)

	tempDir, err := ioutil.TempDir(step.tempDir, "upload")

	if err != nil {
		return err
	}

	defer os.RemoveAll(tempDir)

	currentUser, err := user.Current()
	if err != nil {
		return err
	}

	_, err = step.wardenClient.CopyOut(
		step.containerHandle,
		step.model.From,
		tempDir,
		currentUser.Username,
	)
	if err != nil {
		return emittable_error.New(err, "Copying out of the container failed")
	}

	url, err := url.ParseRequestURI(step.model.To)
	if err != nil {
		return err
	}

	tempFile, err := ioutil.TempFile(step.tempDir, "compressed")
	if err != nil {
		return emittable_error.New(err, "Compression failed")
	}

	tempFile.Close()

	finalFileLocation := tempFile.Name()

	defer os.RemoveAll(finalFileLocation)

	err = step.compressor.Compress(tempDir, finalFileLocation)
	if err != nil {
		return emittable_error.New(err, "Compression failed")
	}

	uploadedBytes, err := step.uploader.Upload(finalFileLocation, url)
	if err != nil {
		return err
	}

	fmt.Fprintf(step.streamer.Stdout(), fmt.Sprintf("Uploaded (%s)\n", bytefmt.ByteSize(uint64(uploadedBytes))))

	return nil
}

func (step *UploadStep) Cancel() {}

func (step *UploadStep) Cleanup() {}
