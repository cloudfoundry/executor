package transformer

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/emit_progress_step"
	"github.com/cloudfoundry-incubator/executor/depot/steps/monitor_step"
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
	"github.com/pivotal-golang/timer"
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

func (transformer *Transformer) StepsFor(
	logStreamer log_streamer.LogStreamer,
	actions []models.ExecutorAction,
	globalEnv []executor.EnvironmentVariable,
	container garden_api.Container,
) ([]sequence.Step, error) {
	subSteps := []sequence.Step{}

	for _, a := range actions {
		step, err := transformer.convertAction(logStreamer, a, globalEnv, container)
		if err != nil {
			return nil, err
		}

		subSteps = append(subSteps, step)
	}

	return subSteps, nil
}

func (transformer *Transformer) convertAction(
	logStreamer log_streamer.LogStreamer,
	action models.ExecutorAction,
	globalEnv []executor.EnvironmentVariable,
	container garden_api.Container,
) (sequence.Step, error) {
	logger := transformer.logger.WithData(lager.Data{
		"handle": container.Handle(),
	})

	switch actionModel := action.Action.(type) {
	case models.RunAction:
		var runEnv []models.EnvironmentVariable
		for _, e := range globalEnv {
			runEnv = append(runEnv, models.EnvironmentVariable{
				Name:  e.Name,
				Value: e.Value,
			})
		}

		actionModel.Env = append(runEnv, actionModel.Env...)

		return run_step.New(
			container,
			actionModel,
			logStreamer,
			logger,
			transformer.allowPrivileged,
		), nil
	case models.DownloadAction:
		return download_step.New(
			container,
			actionModel,
			transformer.cachedDownloader,
			transformer.downloadLimiter,
			logger,
		), nil
	case models.UploadAction:
		return upload_step.New(
			container,
			actionModel,
			transformer.uploader,
			transformer.compressor,
			transformer.tempDir,
			logStreamer,
			transformer.uploadLimiter,
			logger,
		), nil
	case models.EmitProgressAction:
		subStep, err := transformer.convertAction(
			logStreamer,
			actionModel.Action,
			globalEnv,
			container,
		)
		if err != nil {
			return nil, err
		}

		return emit_progress_step.New(
			subStep,
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessage,
			logStreamer,
			logger,
		), nil
	case models.TryAction:
		subStep, err := transformer.convertAction(
			logStreamer,
			actionModel.Action,
			globalEnv,
			container,
		)
		if err != nil {
			return nil, err
		}

		return try_step.New(subStep, logger), nil
	case models.MonitorAction:
		var healthyHook *http.Request
		var unhealthyHook *http.Request

		if actionModel.HealthyHook.URL != "" {
			healthyHookURL, err := url.ParseRequestURI(actionModel.HealthyHook.URL)
			if err != nil {
				return nil, err
			}

			healthyHook = &http.Request{
				Method: actionModel.HealthyHook.Method,
				URL:    healthyHookURL,
			}
		}

		if actionModel.UnhealthyHook.URL != "" {
			unhealthyHookURL, err := url.ParseRequestURI(actionModel.UnhealthyHook.URL)
			if err != nil {
				return nil, err
			}

			unhealthyHook = &http.Request{
				Method: actionModel.UnhealthyHook.Method,
				URL:    unhealthyHookURL,
			}
		}

		check, err := transformer.convertAction(
			logStreamer,
			actionModel.Action,
			globalEnv,
			container,
		)
		if err != nil {
			return nil, err
		}

		return monitor_step.New(
			check,
			actionModel.HealthyThreshold,
			actionModel.UnhealthyThreshold,
			healthyHook,
			unhealthyHook,
			logger,
			timer.NewTimer(),
		), nil
	case models.ParallelAction:
		steps := make([]sequence.Step, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			var err error

			steps[i], err = transformer.convertAction(
				logStreamer,
				action,
				globalEnv,
				container,
			)
			if err != nil {
				return nil, err
			}
		}

		return parallel_step.New(steps), nil
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}
