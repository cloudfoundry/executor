package action_runner

import (
	"errors"
)

type ActionRunner struct {
	actions []Action
	cancel  chan struct{}
}

var CancelledError = errors.New("actions cancelled")

func New(actions []Action) *ActionRunner {
	return &ActionRunner{
		actions: actions,

		cancel: make(chan struct{}),
	}
}

func (runner *ActionRunner) Perform() error {
	var performResult error

	cleanups := []func(){}

actions:
	for _, action := range runner.actions {
		subactionResult := make(chan error, 1)

		go func() {
			subactionResult <- action.Perform()
		}()

		select {
		case err := <-subactionResult:
			if err != nil {
				performResult = err
				break actions
			} else {
				cleanups = append(cleanups, action.Cleanup)
			}

		case <-runner.cancel:
			action.Cancel()
			performResult = CancelledError
			break actions
		}
	}

	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}

	return performResult
}

func (runner *ActionRunner) Cancel() {
	close(runner.cancel)
}

func (runner *ActionRunner) Cleanup() {}
