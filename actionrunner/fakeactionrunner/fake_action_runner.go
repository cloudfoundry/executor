package fakeactionrunner

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"sync"
)

type FakeActionRunner struct {
	containerHandle string
	actions         []models.ExecutorAction
	errChan         chan error
	lock            *sync.Mutex
}

func New() *FakeActionRunner {
	return &FakeActionRunner{
		lock: &sync.Mutex{},
	}
}

func (runner *FakeActionRunner) Run(containerHandle string, actions []models.ExecutorAction) error {
	errChan := make(chan error)
	runner.lock.Lock()
	runner.containerHandle = containerHandle
	runner.actions = actions
	runner.errChan = errChan
	runner.lock.Unlock()
	return <-errChan
}

func (runner *FakeActionRunner) GetContainerHandle() string {
	runner.lock.Lock()
	defer runner.lock.Unlock()
	return runner.containerHandle
}

func (runner *FakeActionRunner) GetActions() []models.ExecutorAction {
	runner.lock.Lock()
	defer runner.lock.Unlock()
	return runner.actions
}

func (runner *FakeActionRunner) ResolveWithError(err error) {
	runner.lock.Lock()
	defer runner.lock.Unlock()
	runner.errChan <- err
}

func (runner *FakeActionRunner) ResolveWithoutError() {
	runner.ResolveWithError(nil)
}
