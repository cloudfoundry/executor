package run_once_transformer

import (
	"fmt"

	"github.com/cloudfoundry-incubator/executor/steps/emit_progress_step"

	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
	"github.com/cloudfoundry-incubator/executor/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/steps/try_step"
	"github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
)

type TaskTransformer struct {
	logStreamerFactory log_streamer_factory.LogStreamerFactory
	downloader         downloader.Downloader
	uploader           uploader.Uploader
	extractor          extractor.Extractor
	compressor         compressor.Compressor
	wardenClient       gordon.Client
	logger             *steno.Logger
	tempDir            string
	result             *string
}

func NewTaskTransformer(
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	downloader downloader.Downloader,
	uploader uploader.Uploader,
	extractor extractor.Extractor,
	compressor compressor.Compressor,
	wardenClient gordon.Client,
	logger *steno.Logger,
	tempDir string,
) *TaskTransformer {
	return &TaskTransformer{
		logStreamerFactory: logStreamerFactory,
		downloader:         downloader,
		uploader:           uploader,
		extractor:          extractor,
		compressor:         compressor,
		wardenClient:       wardenClient,
		logger:             logger,
		tempDir:            tempDir,
	}
}

func (transformer *TaskTransformer) StepsFor(
	task *models.Task,
	containerHandle string,
	result *string,
) []sequence.Step {
	subSteps := []sequence.Step{}

	for _, a := range task.Actions {
		step := transformer.convertAction(task, a, containerHandle, result)
		subSteps = append(subSteps, step)
	}

	return subSteps
}

func (transformer *TaskTransformer) convertAction(
	task *models.Task,
	action models.ExecutorAction,
	containerHandle string,
	result *string,
) sequence.Step {
	logStreamer := transformer.logStreamerFactory(task.Log)

	switch actionModel := action.Action.(type) {
	case models.RunAction:
		return run_step.New(
			containerHandle,
			actionModel,
			task.FileDescriptors,
			logStreamer,
			transformer.wardenClient,
			transformer.logger,
		)
	case models.DownloadAction:
		return download_step.New(
			containerHandle,
			actionModel,
			transformer.downloader,
			transformer.extractor,
			transformer.tempDir,
			transformer.wardenClient,
			logStreamer,
			transformer.logger,
		)
	case models.UploadAction:
		return upload_step.New(
			containerHandle,
			actionModel,
			transformer.uploader,
			transformer.compressor,
			transformer.tempDir,
			transformer.wardenClient,
			logStreamer,
			transformer.logger,
		)
	case models.FetchResultAction:
		return fetch_result_step.New(
			containerHandle,
			actionModel,
			transformer.tempDir,
			transformer.wardenClient,
			transformer.logger,
			result,
		)
	case models.EmitProgressAction:
		return emit_progress_step.New(
			transformer.convertAction(
				task,
				actionModel.Action,
				containerHandle,
				result,
			),
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessage,
			logStreamer,
			transformer.logger)
	case models.TryAction:
		return try_step.New(
			transformer.convertAction(
				task,
				actionModel.Action,
				containerHandle,
				result,
			),
			transformer.logger,
		)
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}
