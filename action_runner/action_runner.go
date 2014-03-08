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

func (runner *ActionRunner) Perform() error {
	var performResult error

	cleanups := []func(){}

	var cancelled chan bool

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

	return performResult
}

func (runner *ActionRunner) Cancel() {
	cancelled := make(chan bool)
	runner.cancel <- cancelled
	<-cancelled
}

func (runner *ActionRunner) Cleanup() {}
