package steps

import (
	"code.cloudfoundry.org/lager"
)

type backgroundStep struct {
	substep Step
	logger  lager.Logger
	*canceller
}

func NewBackground(substep Step, logger lager.Logger) *backgroundStep {
	logger = logger.Session("background-step")
	return &backgroundStep{
		substep:   substep,
		logger:    logger,
		canceller: newCanceller(),
	}
}

func (step *backgroundStep) Perform() error {
	errCh := make(chan error)
	go func() {
		errCh <- step.substep.Perform()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			step.logger.Info("failed", lager.Data{
				"error": err.Error(),
			})

		}
		return err
	case <-step.cancelled:
		step.logger.Info("detaching-from-substep")
		return nil
	}
}
