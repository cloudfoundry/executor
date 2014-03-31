package run_once_transformer

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/compressor"
	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
	"github.com/cloudfoundry-incubator/executor/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader"
)

type RunOnceTransformer struct {
	logStreamerFactory log_streamer_factory.LogStreamerFactory
	downloader         downloader.Downloader
	uploader           uploader.Uploader
	extractor          extractor.Extractor
	compressor         compressor.Compressor
	backendPlugin      backend_plugin.BackendPlugin
	wardenClient       gordon.Client
	logger             *steno.Logger
	tempDir            string
	result             *string
}

func NewRunOnceTransformer(
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	extractor extractor.Extractor,
	compressor compressor.Compressor,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
	tempDir string,
) *RunOnceTransformer {
	return &RunOnceTransformer{
		logStreamerFactory: logStreamerFactory,
		downloader:         downloader,
		uploader:           uploader,
		extractor:          extractor,
		compressor:         compressor,
		backendPlugin:      backendPlugin,
		wardenClient:       wardenClient,
		logger:             logger,
		tempDir:            tempDir,
	}
}

func (transformer *RunOnceTransformer) StepsFor(
	runOnce *models.RunOnce,
	containerHandle *string,
	result *string,
) []sequence.Step {
	logStreamer := transformer.logStreamerFactory(runOnce.Log)

	subSteps := []sequence.Step{}

	var subStep sequence.Step

	for _, a := range runOnce.Actions {
		switch actionModel := a.Action.(type) {
		case models.RunAction:
			subStep = run_step.New(
				*containerHandle,
				actionModel,
				runOnce.FileDescriptors,
				logStreamer,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.DownloadAction:
			subStep = download_step.New(
				*containerHandle,
				actionModel,
				transformer.downloader,
				transformer.extractor,
				transformer.tempDir,
				transformer.backendPlugin,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.UploadAction:
			subStep = upload_step.New(
				*containerHandle,
				actionModel,
				transformer.uploader,
				transformer.compressor,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
			)
		case models.FetchResultAction:
			subStep = fetch_result_step.New(
				*containerHandle,
				actionModel,
				transformer.tempDir,
				transformer.wardenClient,
				transformer.logger,
				result,
			)
		}

		subSteps = append(subSteps, subStep)
	}

	return subSteps
}
