package start_action

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type StartAction struct {
	runOnce         *models.RunOnce
	logger          *steno.Logger
	bbs             Bbs.ExecutorBBS
	containerHandle *string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
	containerHandle *string,
) *StartAction {
	return &StartAction{
		runOnce:         runOnce,
		logger:          logger,
		bbs:             bbs,
		containerHandle: containerHandle,
	}
}

func (action StartAction) Perform() error {
	err := action.bbs.StartRunOnce(action.runOnce, *action.containerHandle)
	if err != nil {
		action.logger.Warnd(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.start.failed",
		)

		return err
	}

	return nil
}

func (action StartAction) Cancel() {}

func (action StartAction) Cleanup() {}
