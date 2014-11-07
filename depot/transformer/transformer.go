package transformer

import (
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/uploader"
	garden "github.com/cloudfoundry-incubator/garden/api"
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
	downloadLimiter  chan struct{}
	uploadLimiter    chan struct{}
	logger           lager.Logger
	tempDir          string
	allowPrivileged  bool
}

func NewTransformer(
	cachedDownloader cacheddownloader.CachedDownloader,
	uploader uploader.Uploader,
	extractor extractor.Extractor,
	compressor compressor.Compressor,
	downloadLimiter chan struct{},
	uploadLimiter chan struct{},
	logger lager.Logger,
	tempDir string,
	allowPrivileged bool,
) *Transformer {
	return &Transformer{
		cachedDownloader: cachedDownloader,
		uploader:         uploader,
		extractor:        extractor,
		compressor:       compressor,
		downloadLimiter:  downloadLimiter,
		uploadLimiter:    uploadLimiter,
		logger:           logger,
		tempDir:          tempDir,
		allowPrivileged:  allowPrivileged,
	}
}

func (transformer *Transformer) StepFor(
	logStreamer log_streamer.LogStreamer,
	action models.ExecutorAction,
	container garden.Container,
) steps.Step {
	logger := transformer.logger.WithData(lager.Data{
		"handle": container.Handle(),
	})

	switch actionModel := action.Action.(type) {
	case models.RunAction:
		return steps.NewRun(
			container,
			actionModel,
			logStreamer.WithSource(actionModel.LogSource),
			logger,
			transformer.allowPrivileged,
		)

	case models.DownloadAction:
		return steps.NewDownload(
			container,
			actionModel,
			transformer.cachedDownloader,
			transformer.downloadLimiter,
			logger,
		)

	case models.UploadAction:
		return steps.NewUpload(
			container,
			actionModel,
			transformer.uploader,
			transformer.compressor,
			transformer.tempDir,
			logStreamer.WithSource(actionModel.LogSource),
			transformer.uploadLimiter,
			logger,
		)

	case models.EmitProgressAction:
		return steps.NewEmitProgress(
			transformer.StepFor(
				logStreamer,
				actionModel.Action,
				container,
			),
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessage,
			logStreamer.WithSource(actionModel.LogSource),
			logger,
		)

	case models.TryAction:
		return steps.NewTry(
			transformer.StepFor(
				logStreamer.WithSource(actionModel.LogSource),
				actionModel.Action,
				container,
			),
			logger,
		)

	case models.ParallelAction:
		subSteps := make([]steps.Step, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			subSteps[i] = transformer.StepFor(
				logStreamer.WithSource(actionModel.LogSource),
				action,
				container,
			)
		}
		return steps.NewParallel(subSteps)

	case models.SerialAction:
		subSteps := make([]steps.Step, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			subSteps[i] = transformer.StepFor(
				logStreamer,
				action,
				container,
			)
		}
		return steps.NewSerial(subSteps)
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}
