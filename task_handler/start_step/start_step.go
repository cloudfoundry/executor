package start_step

import (
	"github.com/cloudfoundry-incubator/garden/warden"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type StartStep struct {
	task      *models.Task
	logger    *steno.Logger
	bbs       Bbs.ExecutorBBS
	container *warden.Container
}

func New(
	task *models.Task,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
	container *warden.Container,
) *StartStep {
	return &StartStep{
		task:      task,
		logger:    logger,
		bbs:       bbs,
		container: container,
	}
}

func (step StartStep) Perform() error {
	err := step.bbs.StartTask(step.task, (*step.container).Handle())
	if err != nil {
		step.logger.Warnd(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":     err.Error(),
			},
			"task.start.failed",
		)

		return err
	}

	return nil
}

func (step StartStep) Cancel() {}

func (step StartStep) Cleanup() {}
