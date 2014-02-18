package fakeactionrunner

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeActionRunner struct {
	ContainerHandle string
	Actions         []models.ExecutorAction
	Streamer        logstreamer.LogStreamer
	RunError        error
}

func New() *FakeActionRunner {
	return &FakeActionRunner{}
}

func (runner *FakeActionRunner) Run(containerHandle string, streamer logstreamer.LogStreamer, actions []models.ExecutorAction) error {
	runner.ContainerHandle = containerHandle
	runner.Streamer = streamer
	runner.Actions = actions
	return runner.RunError
}
