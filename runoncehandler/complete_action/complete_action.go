package complete_action

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type CompleteAction struct {
	runOnce *models.RunOnce
	logger  *steno.Logger
	bbs     Bbs.ExecutorBBS
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	bbs Bbs.ExecutorBBS,
) *CompleteAction {
	return &CompleteAction{
		runOnce: runOnce,
		logger:  logger,
		bbs:     bbs,
	}
}

func (action CompleteAction) Perform(result chan<- error) {
	err := action.bbs.CompleteRunOnce(*action.runOnce)
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.claim.failed",
		)
	}

	result <- err
}

func (action CompleteAction) Cancel() {}

func (action CompleteAction) Cleanup() {}
