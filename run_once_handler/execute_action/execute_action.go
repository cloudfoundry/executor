package execute_action

import (
	"github.com/cloudfoundry-incubator/executor/action_runner"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ExecuteAction struct {
	runOnce *models.RunOnce
	logger  *steno.Logger
	action  action_runner.Action
	bbs     Bbs.ExecutorBBS
	result  *string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	action action_runner.Action,
	bbs Bbs.ExecutorBBS,
	result *string,
) *ExecuteAction {
	return &ExecuteAction{
		runOnce: runOnce,
		logger:  logger,
		action:  action,
		bbs:     bbs,
		result:  result,
	}
}

func (action ExecuteAction) Perform() error {
	err := action.action.Perform()
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"handle":       action.runOnce.ContainerHandle,
				"error":        err.Error(),
			},
			"runonce.actions.failed",
		)

		return action.complete(true, err.Error())
	}

	return action.complete(false, "")
}

func (action ExecuteAction) complete(failed bool, failureReason string) error {
	err := action.bbs.CompleteRunOnce(action.runOnce, failed, failureReason, *action.result)
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.completed.failed",
		)

		return err
	}

	return nil
}

func (action ExecuteAction) Cancel() {
	action.action.Cancel()
}

func (action ExecuteAction) Cleanup() {
	action.action.Cleanup()
}
