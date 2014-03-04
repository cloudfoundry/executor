package register_action

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/taskregistry"
)

type RegisterAction struct {
	runOnce      models.RunOnce
	logger       *steno.Logger
	taskRegistry taskregistry.TaskRegistryInterface
}

func New(
	runOnce models.RunOnce,
	logger *steno.Logger,
	taskRegistry taskregistry.TaskRegistryInterface,
) *RegisterAction {
	return &RegisterAction{
		runOnce:      runOnce,
		logger:       logger,
		taskRegistry: taskRegistry,
	}
}

func (action RegisterAction) Perform(result chan<- error) {
	err := action.taskRegistry.AddRunOnce(action.runOnce)
	if err != nil {
		action.logger.Infod(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.insufficient.resources",
		)
	}

	result <- err
}

func (action RegisterAction) Cancel() {}

func (action RegisterAction) Cleanup() {
	action.taskRegistry.RemoveRunOnce(action.runOnce)
}
