package execute_action

import (
	"strconv"

	"github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/loggregatorlib/emitter"
)

type ExecuteAction struct {
	runOnce           *models.RunOnce
	logger            *steno.Logger
	bbs               Bbs.ExecutorBBS
	actionRunner      actionrunner.ActionRunnerInterface
	loggregatorServer string
	loggregatorSecret string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
	actionRunner actionrunner.ActionRunnerInterface,
	loggregatorServer string,
	loggregatorSecret string,
) *ExecuteAction {
	return &ExecuteAction{
		runOnce:           runOnce,
		logger:            logger,
		bbs:               bbs,
		actionRunner:      actionRunner,
		loggregatorServer: loggregatorServer,
		loggregatorSecret: loggregatorSecret,
	}
}

func (action ExecuteAction) Perform(result chan<- error) {
	err := action.bbs.StartRunOnce(*action.runOnce)
	if err != nil {
		action.logger.Warnd(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.start.failed",
		)
	} else {
		var streamer logstreamer.LogStreamer
		if action.runOnce.Log.SourceName != "" {
			streamer = action.createLogStreamer()
		}

		result, err := action.actionRunner.Run(action.runOnce.ContainerHandle, streamer, action.runOnce.Actions)

		action.runOnce.Result = result
		if err != nil {
			action.logger.Errord(map[string]interface{}{"runonce-guid": action.runOnce.Guid, "handle": action.runOnce.ContainerHandle, "error": err.Error()}, "runonce.actions.failed")
			action.runOnce.Failed = true
			action.runOnce.FailureReason = err.Error()
		}
	}

	result <- err
}

func (action ExecuteAction) Cancel() {}

func (action ExecuteAction) Cleanup() {}

func (action ExecuteAction) createLogStreamer() logstreamer.LogStreamer {
	sourceId := ""
	if action.runOnce.Log.Index != nil {
		sourceId = strconv.Itoa(*action.runOnce.Log.Index)
	}

	logEmitter, _ := emitter.NewEmitter(
		action.loggregatorServer,
		action.runOnce.Log.SourceName,
		sourceId,
		action.loggregatorSecret,
		nil,
	)

	return logstreamer.New(action.runOnce.Log.Guid, logEmitter)
}
