package start_step

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type StartStep struct {
	runOnce         *models.Task
	logger          *steno.Logger
	bbs             Bbs.ExecutorBBS
	containerHandle *string
}

func New(
	runOnce *models.Task,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
	containerHandle *string,
) *StartStep {
	return &StartStep{
		runOnce:         runOnce,
		logger:          logger,
		bbs:             bbs,
		containerHandle: containerHandle,
	}
}

func (step StartStep) Perform() error {
	err := step.bbs.StartTask(step.runOnce, *step.containerHandle)
	if err != nil {
		step.logger.Warnd(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.start.failed",
		)

		return err
	}

	return nil
}

func (step StartStep) Cancel() {}

func (step StartStep) Cleanup() {}
