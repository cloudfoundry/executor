package action_runner

import (
	"errors"
)

type ActionRunner struct {
	actions []Action
	cancel  chan chan bool
}

var CancelledError = errors.New("actions cancelled")

func New(actions []Action) *ActionRunner {
	return &ActionRunner{
		actions: actions,

		cancel: make(chan chan bool),
	}
}

func (runner *ActionRunner) Perform(result chan<- error) {
	var performResult error

	cleanups := []func(){}

	var cancelled chan bool

actions:
	for _, action := range runner.actions {
		subactionResult := make(chan error, 1)
		go action.Perform(subactionResult)

		select {
		case err := <-subactionResult:
			if err != nil {
				performResult = err
				break actions
			} else {
				cleanups = append(cleanups, action.Cleanup)
			}

		case cancelled = <-runner.cancel:
			action.Cancel()
			performResult = CancelledError
			break actions
		}
	}

	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}

	if cancelled != nil {
		cancelled <- true
	}

	result <- performResult
}

func (runner *ActionRunner) Cancel() <-chan bool {
	cancelled := make(chan bool)
	runner.cancel <- cancelled
	return cancelled
}
