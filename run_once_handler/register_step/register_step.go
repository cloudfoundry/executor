package register_step

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/task_registry"
)

type RegisterStep struct {
	task      *models.Task
	logger       *steno.Logger
	taskRegistry task_registry.TaskRegistryInterface
}

func New(
	task *models.Task,
	logger *steno.Logger,
	taskRegistry task_registry.TaskRegistryInterface,
) *RegisterStep {
	return &RegisterStep{
		task:      task,
		logger:       logger,
		taskRegistry: taskRegistry,
	}
}

func (step RegisterStep) Perform() error {
	err := step.taskRegistry.AddTask(step.task)
	if err != nil {
		step.logger.Infod(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":        err.Error(),
			}, "task.insufficient.resources",
		)

		return err
	}

	return nil
}

func (step RegisterStep) Cancel() {}

func (step RegisterStep) Cleanup() {
	step.taskRegistry.RemoveTask(step.task)
}
