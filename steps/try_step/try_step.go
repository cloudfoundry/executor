package try_step

import (
	"github.com/cloudfoundry-incubator/executor/sequence"
	steno "github.com/cloudfoundry/gosteno"
)

type TryStep struct {
	substep sequence.Step
	logger  *steno.Logger
}

func New(substep sequence.Step, logger *steno.Logger) *TryStep {
	return &TryStep{
		substep: substep,
		logger:  logger,
	}
}

func (step *TryStep) Perform() error {
	err := step.substep.Perform()
	if err != nil {
		step.logger.Warnd(
			map[string]interface{}{
				"error": err.Error(),
			},
			"try.failed",
		)
	}

	return nil //We never return an error.  That's the point.
}

func (step *TryStep) Cancel() {}

func (step *TryStep) Cleanup() {}
