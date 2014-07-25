package upload_step

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/bytefmt"
	"github.com/pivotal-golang/lager"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/executor/uploader"
)

type UploadStep struct {
	container  warden.Container
	model      models.UploadAction
	uploader   uploader.Uploader
	compressor compressor.Compressor
	tempDir    string
	streamer   log_streamer.LogStreamer
	logger     lager.Logger
}

func New(
	container warden.Container,
	model models.UploadAction,
	uploader uploader.Uploader,
	compressor compressor.Compressor,
	tempDir string,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
) *UploadStep {
	return &UploadStep{
		container:  container,
		model:      model,
		uploader:   uploader,
		compressor: compressor,
		tempDir:    tempDir,
		streamer:   streamer,
		logger:     logger,
	}
}

func (step *UploadStep) Perform() (err error) {
	url, err := url.ParseRequestURI(step.model.To)
	if err != nil {
		return err
	}

	tempDir, err := ioutil.TempDir(step.tempDir, "upload")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tempDir)

	streamOut, err := step.container.StreamOut(step.model.From)
	if err != nil {
		return emittable_error.New(err, "Copying out of the container failed")
	}
	defer streamOut.Close()

	tempFile, err := ioutil.TempFile(step.tempDir, "compressed")
	if err != nil {
		return emittable_error.New(err, "Compression failed")
	}

	gzipWriter := gzip.NewWriter(tempFile)

	_, err = io.Copy(gzipWriter, streamOut)
	if err != nil {
		return emittable_error.New(err, "Copying out of the container failed")
	}

	gzipWriter.Close()
	tempFile.Close()

	finalFileLocation := tempFile.Name()

	defer os.RemoveAll(finalFileLocation)

	uploadedBytes, err := step.uploader.Upload(finalFileLocation, url, step.logger)
	if err != nil {
		return err
	}

	fmt.Fprintf(step.streamer.Stdout(), fmt.Sprintf("Uploaded (%s)\n", bytefmt.ByteSize(uint64(uploadedBytes))))

	return nil
}

func (step *UploadStep) Cancel() {}

func (step *UploadStep) Cleanup() {}
