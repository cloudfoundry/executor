package runoncehandler

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"strconv"

	"github.com/vito/gordon"
)

type RunOnceHandlerInterface interface {
	RunOnce(runOnce models.RunOnce, executorId string)
}

type RunOnceHandler struct {
	bbs          Bbs.ExecutorBBS
	wardenClient gordon.Client
	actionRunner actionrunner.ActionRunnerInterface

	loggregatorServer string
	loggregatorSecret string

	logger *steno.Logger

	taskRegistry taskregistry.TaskRegistryInterface

	stack string
}

func New(
	bbs Bbs.ExecutorBBS,
	wardenClient gordon.Client,
	taskRegistry taskregistry.TaskRegistryInterface,
	actionRunner actionrunner.ActionRunnerInterface,
	loggregatorServer string,
	loggregatorSecret string,
	stack string,
	logger *steno.Logger,
) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:               bbs,
		wardenClient:      wardenClient,
		taskRegistry:      taskRegistry,
		actionRunner:      actionRunner,
		loggregatorServer: loggregatorServer,
		loggregatorSecret: loggregatorSecret,
		logger:            logger,
		stack:             stack,
	}
}

func (handler *RunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string) {
	// check for stack compatibility
	if runOnce.Stack != "" && handler.stack != runOnce.Stack {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "desired-stack": runOnce.Stack, "executor-stack": handler.stack}, "runonce.stack.mismatch")
		return
	}

	// reserve resources
	err := handler.taskRegistry.AddRunOnce(runOnce)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "error": err.Error()}, "runonce.insufficient.resources")
		return
	}
	defer handler.taskRegistry.RemoveRunOnce(runOnce)

	// claim the RunOnce
	runOnce.ExecutorID = executorId
	handler.logger.Infod(map[string]interface{}{"runonce-guid": runOnce.Guid}, "runonce.claim")

	err = handler.bbs.ClaimRunOnce(runOnce)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "error": err.Error()}, "runonce.claim.failed")
		return
	}

	// create the container
	createResponse, err := handler.wardenClient.Create()
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "error": err.Error()}, "runonce.container-create.failed")
		return
	}
	runOnce.ContainerHandle = createResponse.GetHandle()
	handler.logger.Infod(map[string]interface{}{"runonce-guid": runOnce.Guid, "handle": runOnce.ContainerHandle}, "runonce.container-create.success")
	defer func() {
		_, err := handler.wardenClient.Destroy(runOnce.ContainerHandle)
		if err != nil {
			handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "handle": runOnce.ContainerHandle, "error": err.Error()}, "runonce.container-destroy.failed")
		}
	}()

	// mark the RunOnce as started
	handler.logger.Infod(map[string]interface{}{"runonce-guid": runOnce.Guid}, "runonce.start")
	err = handler.bbs.StartRunOnce(runOnce)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "error": err.Error()}, "runonce.start.failed")
		return
	}

	var streamer logstreamer.LogStreamer

	if runOnce.Log.SourceName != "" {
		sourceId := ""
		if runOnce.Log.Index != nil {
			sourceId = strconv.Itoa(*runOnce.Log.Index)
		}
		logEmitter, _ := emitter.NewEmitter(handler.loggregatorServer, runOnce.Log.SourceName, sourceId, handler.loggregatorSecret, nil)
		streamer = logstreamer.New(runOnce.Log.Guid, logEmitter)
	}

	// perform the actions
	result, err := handler.actionRunner.Run(runOnce.ContainerHandle, streamer, runOnce.Actions)
	runOnce.Result = result
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "handle": runOnce.ContainerHandle, "error": err.Error()}, "runonce.actions.failed")
		runOnce.Failed = true
		runOnce.FailureReason = err.Error()
	}

	// mark the task as completed
	handler.logger.Infod(map[string]interface{}{"runonce-guid": runOnce.Guid}, "runonce.complete")
	err = handler.bbs.CompleteRunOnce(runOnce)
	if err != nil {
		handler.logger.Errord(map[string]interface{}{"runonce-guid": runOnce.Guid, "error": err.Error()}, "runonce.complete.failed")
		return
	}
}
