package steps

import (

	"github.com/pivotal-golang/lager"
)

type TryStep struct {
	substep Step
	logger  lager.Logger
}

func NewTry(substep Step, logger lager.Logger) *TryStep {
	logger = logger.Session("TryAction")
	return &TryStep{
		substep: substep,
		logger:  logger,
	}
}

func (step *TryStep) Perform() error {
	err := step.substep.Perform()
	if err != nil {
		step.logger.Info("failed", lager.Data{
			"error": err.Error(),
		})
	}

	return nil //We never return an error.  That's the point.
}

func (step *TryStep) Cancel() {
	step.substep.Cancel()
}

func (step *TryStep) Cleanup() {
	step.substep.Cleanup()
}
