package start_action

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type StartAction struct {
	runOnce *models.RunOnce
	logger  *steno.Logger
	bbs     Bbs.ExecutorBBS
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
) *StartAction {
	return &StartAction{
		runOnce: runOnce,
		logger:  logger,
		bbs:     bbs,
	}
}

func (action StartAction) Perform(result chan<- error) {
	err := action.bbs.StartRunOnce(*action.runOnce)

	if err != nil {
		action.logger.Warnd(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.start.failed",
		)
	}

	result <- err
}

func (action StartAction) Cancel() {}

func (action StartAction) Cleanup() {}
