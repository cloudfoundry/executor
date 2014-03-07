package fakeactionrunner

import (
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeActionRunner struct {
	RunOnce   *models.RunOnce
	Actions   []models.ExecutorAction
	Streamer  logstreamer.LogStreamer
	RunError  error
	RunResult string
}

func New() *FakeActionRunner {
	return &FakeActionRunner{}
}

func (runner *FakeActionRunner) Run(runOnce *models.RunOnce, streamer logstreamer.LogStreamer, actions []models.ExecutorAction) (string, error) {
	runner.RunOnce = runOnce
	runner.Streamer = streamer
	runner.Actions = actions
	return runner.RunResult, runner.RunError
}
