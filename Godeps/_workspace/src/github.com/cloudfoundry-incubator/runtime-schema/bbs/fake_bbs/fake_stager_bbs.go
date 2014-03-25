package fake_bbs

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeStagerBBS struct {
	watchingForCompleted    chan bool
	completedRunOnceChan    chan *models.RunOnce
	completedRunOnceErrChan chan error

	whenSettingResolving  func() error
	resolvingRunOnceInput struct {
		RunOnceToResolve *models.RunOnce
	}

	resolvedRunOnce   *models.RunOnce
	resolveRunOnceErr error

	sync.RWMutex
}

func NewFakeStagerBBS() *FakeStagerBBS {
	return &FakeStagerBBS{
		watchingForCompleted: make(chan bool),
	}
}

func (fakeBBS *FakeStagerBBS) WatchForCompletedRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	completedChan := make(chan *models.RunOnce)
	completedErrChan := make(chan error)

	fakeBBS.Lock()
	fakeBBS.completedRunOnceChan = completedChan
	fakeBBS.completedRunOnceErrChan = completedErrChan
	fakeBBS.Unlock()

	fakeBBS.watchingForCompleted <- true

	return completedChan, nil, completedErrChan
}

func (fakeBBS *FakeStagerBBS) ResolvingRunOnce(runOnce *models.RunOnce) error {
	fakeBBS.RLock()
	callback := fakeBBS.whenSettingResolving
	fakeBBS.RUnlock()

	if callback != nil {
		err := callback()
		if err != nil {
			return err
		}
	}

	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.resolvingRunOnceInput.RunOnceToResolve = runOnce

	return nil
}

func (fakeBBS *FakeStagerBBS) DesireRunOnce(runOnce *models.RunOnce) error {
	panic("implement me!")
}

func (fakeBBS *FakeStagerBBS) ResolveRunOnce(runOnce *models.RunOnce) error {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	if fakeBBS.resolveRunOnceErr != nil {
		return fakeBBS.resolveRunOnceErr
	}

	fakeBBS.resolvedRunOnce = runOnce

	return nil
}

func (fakeBBS *FakeStagerBBS) GetAvailableFileServer() (string, error) {
	panic("implement me!")
}

func (fakeBBS *FakeStagerBBS) SendCompletedRunOnce(runOnce *models.RunOnce) {
	fakeBBS.completedRunOnceChan <- runOnce
}

func (fakeBBS *FakeStagerBBS) SendCompletedRunOnceWatchError(err error) {
	fakeBBS.completedRunOnceErrChan <- errors.New("hell")
}

func (fakeBBS *FakeStagerBBS) WatchingForCompleted() <-chan bool {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.watchingForCompleted
}

func (fakeBBS *FakeStagerBBS) WhenSettingResolving(callback func() error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.whenSettingResolving = callback
}

func (fakeBBS *FakeStagerBBS) ResolvingRunOnceInput() *models.RunOnce {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.resolvingRunOnceInput.RunOnceToResolve
}

func (fakeBBS *FakeStagerBBS) ResolvedRunOnce() *models.RunOnce {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.resolvedRunOnce
}
