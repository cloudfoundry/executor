package register_action

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/task_registry"
)

type RegisterAction struct {
	runOnce      models.RunOnce
	logger       *steno.Logger
	taskRegistry task_registry.TaskRegistryInterface
}

func New(
	runOnce models.RunOnce,
	logger *steno.Logger,
	taskRegistry task_registry.TaskRegistryInterface,
) *RegisterAction {
	return &RegisterAction{
		runOnce:      runOnce,
		logger:       logger,
		taskRegistry: taskRegistry,
	}
}

func (action RegisterAction) Perform() error {
	err := action.taskRegistry.AddRunOnce(action.runOnce)
	if err != nil {
		action.logger.Infod(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.insufficient.resources",
		)

		return err
	}

	return nil
}

func (action RegisterAction) Cancel() {}

func (action RegisterAction) Cleanup() {
	action.taskRegistry.RemoveRunOnce(action.runOnce)
}
