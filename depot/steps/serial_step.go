package steps

import (
	"errors"
)

type SerialStep struct {
	steps  []Step
	cancel chan struct{}
}

var CancelledError = errors.New("steps cancelled")

func NewSerial(steps []Step) *SerialStep {
	return &SerialStep{
		steps: steps,

		cancel: make(chan struct{}),
	}
}

func (runner *SerialStep) Perform() error {
	for _, action := range runner.steps {
		subactionResult := make(chan error, 1)

		go func() {
			subactionResult <- action.Perform()
		}()

		select {
		case err := <-subactionResult:
			if err != nil {
				return err
			}

		case <-runner.cancel:
			action.Cancel()
			return CancelledError
		}
	}

	return nil
}

func (runner *SerialStep) Cancel() {
	close(runner.cancel)
}
