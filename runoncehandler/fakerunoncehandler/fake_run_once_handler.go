package fakerunoncehandler

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeRunOnceHandler struct {
	NumberOfCalls   int
	HandledRunOnces map[string]string
}

func New() *FakeRunOnceHandler {
	return &FakeRunOnceHandler{
		HandledRunOnces: make(map[string]string),
	}
}

func (handler *FakeRunOnceHandler) RunOnce(runOnce models.RunOnce, executorId string) {
	_, present := handler.HandledRunOnces[runOnce.Guid]
	if !present {
		handler.NumberOfCalls++
		handler.HandledRunOnces[runOnce.Guid] = executorId
	}
}
