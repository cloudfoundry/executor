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

	taskRegistry *taskregistry.TaskRegistry
}

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client, taskRegistry *taskregistry.TaskRegistry, actionRunner actionrunner.ActionRunnerInterface) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:          bbs,
		wardenClient: wardenClient,
		taskRegistry: taskRegistry,
		actionRunner: actionRunner,
		logger:       steno.NewLogger("RunOnceHandler"),
	}
}

func (handler *RunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string) {
	// reserve resources
	if !handler.taskRegistry.AddRunOnce(runOnce) {
		handler.logger.Errorf("insufficient resources to claim run once: Desired %d (memory) %d (disk).  Have %d (memory) %d (disk).", runOnce.MemoryMB, runOnce.DiskMB, handler.taskRegistry.AvailableMemoryMB(), handler.taskRegistry.AvailableDiskMB())
		return
	}
	defer handler.taskRegistry.RemoveRunOnce(runOnce)

	// claim the RunOnce
	runOnce.ExecutorID = executorId
	err := handler.bbs.ClaimRunOnce(runOnce)
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
	err = handler.bbs.CompleteRunOnce(runOnce)
	if err != nil {
		handler.logger.Errorf("failed to transition RunOnce to completed state: %s", err.Error())
		return
	}
}
