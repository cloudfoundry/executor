package claim_step

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ClaimStep struct {
	task    *models.Task
	logger     *steno.Logger
	executorID string
	bbs        Bbs.ExecutorBBS
}

func New(
	task *models.Task,
	logger *steno.Logger,
	executorID string,
	bbs Bbs.ExecutorBBS,
) *ClaimStep {
	return &ClaimStep{
		task:    task,
		logger:     logger,
		executorID: executorID,
		bbs:        bbs,
	}
}

func (step ClaimStep) Perform() error {
	err := step.bbs.ClaimTask(step.task, step.executorID)
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":        err.Error(),
			}, "task.claim.failed",
		)

		return err
	}

	return nil
}

func (step ClaimStep) Cancel() {}

func (step ClaimStep) Cleanup() {}
