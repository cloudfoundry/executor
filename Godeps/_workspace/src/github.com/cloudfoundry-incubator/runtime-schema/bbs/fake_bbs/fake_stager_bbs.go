package fake_bbs

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeStagerBBS struct {
	FileServerGetter

	watchingForCompleted chan bool
	completedTaskChan    chan models.Task
	completedTaskErrChan chan error

	whenSettingResolving func() error
	resolvingTaskInput   struct {
		TaskToResolve models.Task
	}

	resolvedTask   models.Task
	resolveTaskErr error

	WhenDesiringTask func(models.Task) (models.Task, error)

	sync.RWMutex
}

func NewFakeStagerBBS() *FakeStagerBBS {
	return &FakeStagerBBS{
		watchingForCompleted: make(chan bool),
	}
}

func (fakeBBS *FakeStagerBBS) WatchForCompletedTask() (<-chan models.Task, chan<- bool, <-chan error) {
	completedChan := make(chan models.Task)
	completedErrChan := make(chan error)
	doneChan := make(chan bool)

	fakeBBS.Lock()
	fakeBBS.completedTaskChan = completedChan
	fakeBBS.completedTaskErrChan = completedErrChan
	fakeBBS.Unlock()

	fakeBBS.watchingForCompleted <- true

	return completedChan, doneChan, completedErrChan
}

func (fakeBBS *FakeStagerBBS) ResolvingTask(task models.Task) (models.Task, error) {
	fakeBBS.RLock()
	callback := fakeBBS.whenSettingResolving
	fakeBBS.RUnlock()

	if callback != nil {
		err := callback()
		if err != nil {
			return task, err
		}
	}

	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	fakeBBS.resolvingTaskInput.TaskToResolve = task

	return task, nil
}

func (fakeBBS *FakeStagerBBS) DesireTask(task models.Task) (models.Task, error) {
	return fakeBBS.WhenDesiringTask(task)
}

func (fakeBBS *FakeStagerBBS) ResolveTask(task models.Task) (models.Task, error) {
	fakeBBS.Lock()
	defer fakeBBS.Unlock()

	if fakeBBS.resolveTaskErr != nil {
		return task, fakeBBS.resolveTaskErr
	}

	fakeBBS.resolvedTask = task

	return task, nil
}

func (fakeBBS *FakeStagerBBS) SendCompletedTask(task models.Task) {
	fakeBBS.completedTaskChan <- task
}

func (fakeBBS *FakeStagerBBS) SendCompletedTaskWatchError(err error) {
	fakeBBS.completedTaskErrChan <- errors.New("hell")
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

func (fakeBBS *FakeStagerBBS) ResolvingTaskInput() models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.resolvingTaskInput.TaskToResolve
}

func (fakeBBS *FakeStagerBBS) ResolvedTask() models.Task {
	fakeBBS.RLock()
	defer fakeBBS.RUnlock()

	return fakeBBS.resolvedTask
}
