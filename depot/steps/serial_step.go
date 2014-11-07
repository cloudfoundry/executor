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
	var performResult error

	cleanups := []func(){}

steps:
	for _, action := range runner.steps {
		subactionResult := make(chan error, 1)

		go func() {
			subactionResult <- action.Perform()
		}()

		select {
		case err := <-subactionResult:
			if err != nil {
				performResult = err
				break steps
			} else {
				cleanups = append(cleanups, action.Cleanup)
			}

		case <-runner.cancel:
			action.Cancel()
			performResult = CancelledError
			break steps
		}
	}

	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}

	return performResult
}

func (runner *SerialStep) Cancel() {
	close(runner.cancel)
}

func (runner *SerialStep) Cleanup() {}
