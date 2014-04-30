package task_transformer

import (
	"fmt"

	"github.com/cloudfoundry-incubator/executor/steps/emit_progress_step"

	"github.com/cloudfoundry-incubator/garden/warden"
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
	logger *steno.Logger,
	tempDir string,
) *TaskTransformer {
	return &TaskTransformer{
		logStreamerFactory: logStreamerFactory,
		downloader:         downloader,
		uploader:           uploader,
		extractor:          extractor,
		compressor:         compressor,
		logger:             logger,
		tempDir:            tempDir,
	}
}

func (transformer *TaskTransformer) StepsFor(
	task *models.Task,
	container warden.Container,
	result *string,
) []sequence.Step {
	subSteps := []sequence.Step{}

	for _, a := range task.Actions {
		step := transformer.convertAction(task, a, container, result)
		subSteps = append(subSteps, step)
	}

	return subSteps
}

func (transformer *TaskTransformer) convertAction(
	task *models.Task,
	action models.ExecutorAction,
	container warden.Container,
	result *string,
) sequence.Step {
	logStreamer := transformer.logStreamerFactory(task.Log)

	switch actionModel := action.Action.(type) {
	case models.RunAction:
		return run_step.New(
			container,
			actionModel,
			uint64(task.FileDescriptors), // TODO
			logStreamer,
			transformer.logger,
		)
	case models.DownloadAction:
		return download_step.New(
			container,
			actionModel,
			transformer.downloader,
			transformer.extractor,
			transformer.tempDir,
			logStreamer,
			transformer.logger,
		)
	case models.UploadAction:
		return upload_step.New(
			container,
			actionModel,
			transformer.uploader,
			transformer.compressor,
			transformer.tempDir,
			logStreamer,
			transformer.logger,
		)
	case models.FetchResultAction:
		return fetch_result_step.New(
			container,
			actionModel,
			transformer.tempDir,
			transformer.logger,
			result,
		)
	case models.EmitProgressAction:
		return emit_progress_step.New(
			transformer.convertAction(
				task,
				actionModel.Action,
				container,
				result,
			),
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessage,
			logStreamer,
			transformer.logger,
		)
	case models.TryAction:
		return try_step.New(
			transformer.convertAction(
				task,
				actionModel.Action,
				container,
				result,
			),
			transformer.logger,
		)
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}
