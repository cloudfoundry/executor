package fake_run_once_handler

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"sync"
)

type FakeRunOnceHandler struct {
	numberOfCalls   int
	handledRunOnces map[string]string
	mutex           *sync.Mutex
	cancel          <-chan struct{}
}

func New() *FakeRunOnceHandler {
	return &FakeRunOnceHandler{
		handledRunOnces: make(map[string]string),
		mutex:           &sync.Mutex{},
	}
}

func (handler *FakeRunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string, cancel <-chan struct{}) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	handler.cancel = cancel

	_, present := handler.handledRunOnces[runOnce.Guid]
	if !present {
		handler.numberOfCalls++
		handler.handledRunOnces[runOnce.Guid] = executorId
	}
}

func (handler *FakeRunOnceHandler) NumberOfCalls() int {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	return handler.numberOfCalls
}

func (handler *FakeRunOnceHandler) HandledRunOnces() map[string]string {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	return handler.handledRunOnces
}

func (handler *FakeRunOnceHandler) GetCancel() <-chan struct{} {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	return handler.cancel
}
