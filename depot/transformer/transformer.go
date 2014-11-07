package transformer

import (
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/emit_progress_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/parallel_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/run_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/try_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/depot/uploader"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

var ErrNoCheck = errors.New("no check configured")

type Transformer struct {
	cachedDownloader cacheddownloader.CachedDownloader
	uploader         uploader.Uploader
	extractor        extractor.Extractor
	compressor       compressor.Compressor
	logger           lager.Logger
	tempDir          string
}

func NewTransformer(
	cachedDownloader cacheddownloader.CachedDownloader,
	uploader uploader.Uploader,
	extractor extractor.Extractor,
	compressor compressor.Compressor,
	logger lager.Logger,
	tempDir string,
) *Transformer {
	return &Transformer{
		cachedDownloader: cachedDownloader,
		uploader:         uploader,
		extractor:        extractor,
		compressor:       compressor,
		logger:           logger,
		tempDir:          tempDir,
	}
}

func (transformer *Transformer) StepFor(
	logStreamer log_streamer.LogStreamer,
	action models.ExecutorAction,
	container garden_api.Container,
) sequence.Step {
	logger := transformer.logger.WithData(lager.Data{
		"handle": container.Handle(),
	})

	switch actionModel := action.Action.(type) {
	case models.RunAction:
		return run_step.New(
			container,
			actionModel,
			logStreamer,
			logger,
		)
	case models.DownloadAction:
		return download_step.New(
			container,
			actionModel,
			transformer.cachedDownloader,
			logger,
		)
	case models.UploadAction:
		return upload_step.New(
			container,
			actionModel,
			transformer.uploader,
			transformer.compressor,
			transformer.tempDir,
			logStreamer,
			logger,
		)
	case models.EmitProgressAction:
		return emit_progress_step.New(
			transformer.StepFor(
				logStreamer,
				actionModel.Action,
				container,
			),
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessage,
			logStreamer,
			logger,
		)
	case models.TryAction:
		return try_step.New(
			transformer.StepFor(
				logStreamer,
				actionModel.Action,
				container,
			),
			logger,
		)
	case models.ParallelAction:
		steps := make([]sequence.Step, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			steps[i] = transformer.StepFor(
				logStreamer,
				action,
				container,
			)
		}

		return parallel_step.New(steps)
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}
