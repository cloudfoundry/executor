package fakeactionrunner

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner/emitter"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeActionRunner struct {
	ContainerHandle string
	Actions         []models.ExecutorAction
	Emitter         emitter.Emitter
	RunError        error
}

func New() *FakeActionRunner {
	return &FakeActionRunner{}
}

func (runner *FakeActionRunner) Run(containerHandle string, emitter emitter.Emitter, actions []models.ExecutorAction) error {
	runner.ContainerHandle = containerHandle
	runner.Emitter = emitter
	runner.Actions = actions
	return runner.RunError
}
