package claim_step

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ClaimStep struct {
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
) *ClaimStep {
	return &ClaimStep{
		runOnce:    runOnce,
		logger:     logger,
		executorID: executorID,
		bbs:        bbs,
	}
}

func (step ClaimStep) Perform() error {
	err := step.bbs.ClaimRunOnce(step.runOnce, step.executorID)
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			}, "runonce.claim.failed",
		)

		return err
	}

	return nil
}

func (step ClaimStep) Cancel() {}

func (step ClaimStep) Cleanup() {}
