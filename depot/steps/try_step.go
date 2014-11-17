package steps

import (
	"github.com/pivotal-golang/lager"
)

type tryStep struct {
	substep Step
	logger  lager.Logger
}

func NewTry(substep Step, logger lager.Logger) *tryStep {
	logger = logger.Session("TryAction")
	return &tryStep{
		substep: substep,
		logger:  logger,
	}
}

func (step *tryStep) Perform() error {
	err := step.substep.Perform()
	if err != nil {
		step.logger.Info("failed", lager.Data{
			"error": err.Error(),
		})
	}

	return nil //We never return an error.  That's the point.
}

func (step *tryStep) Cancel() {
	step.substep.Cancel()
}
