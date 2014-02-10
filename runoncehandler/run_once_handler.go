package runoncehandler

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/vito/gordon"
)

type RunOnceHandlerInterface interface {
	RunOnce(runOnce models.RunOnce, executorId string)
}

type RunOnceHandler struct {
	bbs          Bbs.ExecutorBBS
	wardenClient gordon.Client
	actionRunner actionrunner.ActionRunnerInterface

	logger *steno.Logger

	taskRegistry taskregistry.TaskRegistryInterface

	stack string
}

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client, taskRegistry taskregistry.TaskRegistryInterface, actionRunner actionrunner.ActionRunnerInterface, stack string) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:          bbs,
		wardenClient: wardenClient,
		taskRegistry: taskRegistry,
		actionRunner: actionRunner,
		logger:       steno.NewLogger("RunOnceHandler"),
		stack:        stack,
	}
}

func (handler *RunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string) {
	// check for stack compatibility
	if runOnce.Stack != "" && handler.stack != runOnce.Stack {
		handler.logger.Infof("runonce.stack.mismatch - RunOnce stack:%s, Executor stack:%s", runOnce.Stack, handler.stack)
		return
	}

	// reserve resources
	err := handler.taskRegistry.AddRunOnce(runOnce)
	if err != nil {
		handler.logger.Errorf("failed to allocate resources for run once: %s", err.Error())
		return
	}
	defer handler.taskRegistry.RemoveRunOnce(runOnce)

	// claim the RunOnce
	runOnce.ExecutorID = executorId
	handler.logger.Infof("executor.claiming.runonce: %s", runOnce.Guid)
	err = handler.bbs.ClaimRunOnce(runOnce)
	if err != nil {
		handler.logger.Errorf("failed claim run once: %s", err.Error())
		return
	}

	// create the container
	createResponse, err := handler.wardenClient.Create()
	if err != nil {
		handler.logger.Errorf("failed to create warden container: %s", err.Error())
		return
	}
	runOnce.ContainerHandle = createResponse.GetHandle()
	defer func() {
		_, err := handler.wardenClient.Destroy(runOnce.ContainerHandle)
		if err != nil {
			handler.logger.Errord(map[string]interface{}{
				"ContainerHandle": runOnce.ContainerHandle,
			}, "failed to destroy container")
		}
	}()

	// mark the RunOnce as started
	handler.logger.Infof("executor.starting.runonce: %s", runOnce.Guid)
	err = handler.bbs.StartRunOnce(runOnce)
	if err != nil {
		handler.logger.Errorf("failed to transition RunOnce to running state: %s", err.Error())
		return
	}

	// perform the actions
	err = handler.actionRunner.Run(runOnce.ContainerHandle, runOnce.Actions)
	if err != nil {
		handler.logger.Errorf("failed to run RunOnce actions: %s", err.Error())
		runOnce.Failed = true
		runOnce.FailureReason = err.Error()
	}

	// mark the task as completed
	handler.logger.Infof("executor.completed.runonce: %s", runOnce.Guid)
	err = handler.bbs.CompleteRunOnce(runOnce)
	if err != nil {
		handler.logger.Errorf("failed to transition RunOnce to completed state: %s", err.Error())
		return
	}
}
