package execute_step

import (
	"github.com/cloudfoundry-incubator/executor/sequence"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ExecuteStep struct {
	task *models.Task
	logger  *steno.Logger
	subStep sequence.Step
	bbs     Bbs.ExecutorBBS
	result  *string
}

func New(
	task *models.Task,
	logger *steno.Logger,
	subStep sequence.Step,
	bbs Bbs.ExecutorBBS,
	result *string,
) *ExecuteStep {
	return &ExecuteStep{
		task: task,
		logger:  logger,
		subStep: subStep,
		bbs:     bbs,
		result:  result,
	}
}

func (step ExecuteStep) Perform() error {
	err := step.subStep.Perform()
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.task.Guid,
				"handle":       step.task.ContainerHandle,
				"error":        err.Error(),
			},
			"runonce.steps.failed",
		)

		return step.complete(true, err.Error())
	}

	return step.complete(false, "")
}

func (step ExecuteStep) complete(failed bool, failureReason string) error {
	err := step.bbs.CompleteTask(step.task, failed, failureReason, *step.result)
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.task.Guid,
				"error":        err.Error(),
			}, "runonce.completed.failed",
		)

		return err
	}

	return nil
}

func (step ExecuteStep) Cancel() {
	step.subStep.Cancel()
}

func (step ExecuteStep) Cleanup() {
	step.subStep.Cleanup()
}
