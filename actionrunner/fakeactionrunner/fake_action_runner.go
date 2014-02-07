package fakeactionrunner

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeActionRunner struct {
	ContainerHandle string
	Actions         []models.ExecutorAction
	RunError        error
}

func New() *FakeActionRunner {
	return &FakeActionRunner{}
}

func (runner *FakeActionRunner) Run(containerHandle string, actions []models.ExecutorAction) error {
	runner.ContainerHandle = containerHandle
	runner.Actions = actions
	return runner.RunError
}
