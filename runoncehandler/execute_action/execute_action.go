package execute_action

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ExecuteAction struct {
	runOnce           *models.RunOnce
	logger            *steno.Logger
	actionRunner      actionrunner.ActionRunnerInterface
	loggregatorServer string
	loggregatorSecret string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	actionRunner actionrunner.ActionRunnerInterface,
	loggregatorServer string,
	loggregatorSecret string,
) *ExecuteAction {
	return &ExecuteAction{
		runOnce:           runOnce,
		logger:            logger,
		actionRunner:      actionRunner,
		loggregatorServer: loggregatorServer,
		loggregatorSecret: loggregatorSecret,
	}
}

func (action ExecuteAction) Perform(result chan<- error) {
	var logStreamer logstreamer.LogStreamer
	if action.runOnce.Log.SourceName != "" {
		logStreamerFactory := log_streamer_factory.New(
			action.loggregatorServer,
			action.loggregatorSecret,
		)
		logStreamer = logStreamerFactory.Make(action.runOnce.Log)
	}

	executionResult, err := action.actionRunner.Run(
		action.runOnce.ContainerHandle,
		logStreamer,
		action.runOnce.Actions,
	)

	action.runOnce.Result = executionResult
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"handle":       action.runOnce.ContainerHandle,
				"error":        err.Error(),
			},
			"runonce.actions.failed",
		)

		action.runOnce.Failed = true
		action.runOnce.FailureReason = err.Error()
	}

	result <- nil
}

func (action ExecuteAction) Cancel() {}

func (action ExecuteAction) Cleanup() {}
