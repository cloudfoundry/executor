package register_step

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/task_registry"
)

type RegisterStep struct {
	runOnce      *models.Task
	logger       *steno.Logger
	taskRegistry task_registry.TaskRegistryInterface
}

func New(
	runOnce *models.Task,
	logger *steno.Logger,
	taskRegistry task_registry.TaskRegistryInterface,
) *RegisterStep {
	return &RegisterStep{
		runOnce:      runOnce,
		logger:       logger,
		taskRegistry: taskRegistry,
	}
}

func (step RegisterStep) Perform() error {
	err := step.taskRegistry.AddTask(step.runOnce)
	if err != nil {
		step.logger.Infod(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.insufficient.resources",
		)

		return err
	}

	return nil
}

func (step RegisterStep) Cancel() {}

func (step RegisterStep) Cleanup() {
	step.taskRegistry.RemoveTask(step.runOnce)
}
