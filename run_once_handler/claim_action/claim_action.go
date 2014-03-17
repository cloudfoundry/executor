package claim_action

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ClaimAction struct {
	runOnce    *models.RunOnce
	logger     *steno.Logger
	executorID string
	bbs        Bbs.ExecutorBBS
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	executorID string,
	bbs Bbs.ExecutorBBS,
) *ClaimAction {
	return &ClaimAction{
		runOnce:    runOnce,
		logger:     logger,
		executorID: executorID,
		bbs:        bbs,
	}
}

func (action ClaimAction) Perform() error {
	action.runOnce.ExecutorID = action.executorID

	err := action.bbs.ClaimRunOnce(*action.runOnce)
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.claim.failed",
		)

		return err
	}

	return nil
}

func (action ClaimAction) Cancel() {}

func (action ClaimAction) Cleanup() {}
