package sequence

import (
	"errors"
)

type Sequence struct {
	steps  []Step
	cancel chan struct{}
}

var CancelledError = errors.New("steps cancelled")

func New(steps []Step) *Sequence {
	return &Sequence{
		steps: steps,

		cancel: make(chan struct{}),
	}
}

func (runner *Sequence) Perform() error {
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

func (runner *Sequence) Cancel() {
	close(runner.cancel)
}
