package fakerunoncehandler

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"sync"
)

type FakeRunOnceHandler struct {
	numberOfCalls   int
	handledRunOnces map[string]string
	Lock            *sync.Mutex
}

func New() *FakeRunOnceHandler {
	return &FakeRunOnceHandler{
		handledRunOnces: make(map[string]string),
		Lock:            &sync.Mutex{},
	}
}

func (handler *FakeRunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string) {
	handler.Lock.Lock()
	defer handler.Lock.Unlock()

	_, present := handler.handledRunOnces[runOnce.Guid]
	if !present {
		handler.numberOfCalls++
		handler.handledRunOnces[runOnce.Guid] = executorId
	}
}

func (handler *FakeRunOnceHandler) NumberOfCalls() int {
	handler.Lock.Lock()
	defer handler.Lock.Unlock()

	return handler.numberOfCalls
}

func (handler *FakeRunOnceHandler) HandledRunOnces() map[string]string {
	handler.Lock.Lock()
	defer handler.Lock.Unlock()

	return handler.handledRunOnces
}
