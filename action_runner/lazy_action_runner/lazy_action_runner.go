package lazy_action_runner

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor/action_runner"
)

type ActionsGenerator func() []action_runner.Action

type LazyActionRunner struct {
	actionsGenerator ActionsGenerator

	cancelled bool

	actionRunner *action_runner.ActionRunner

	actionMutex *sync.Mutex
}

func New(actionsGenerator ActionsGenerator) action_runner.Action {
	return &LazyActionRunner{
		actionsGenerator: actionsGenerator,

		actionMutex: &sync.Mutex{},
	}
}

func (runner *LazyActionRunner) Perform() error {
	runner.actionMutex.Lock()

	if runner.cancelled {
		runner.actionMutex.Unlock()
		return action_runner.CancelledError
	}

	runner.actionRunner = action_runner.New(runner.actionsGenerator())
	runner.actionMutex.Unlock()

	return runner.actionRunner.Perform()
}

func (runner *LazyActionRunner) Cancel() {
	runner.actionMutex.Lock()
	action := runner.actionRunner
	runner.cancelled = true
	runner.actionMutex.Unlock()

	if action != nil {
		action.Cancel()
	}
}

func (runner *LazyActionRunner) Cleanup() {
	runner.actionMutex.Lock()
	action := runner.actionRunner
	runner.actionMutex.Unlock()

	if action != nil {
		action.Cleanup()
	}
}
