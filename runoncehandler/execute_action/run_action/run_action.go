package run_action

import (
	"fmt"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type RunAction struct {
	model           models.RunAction
	containerHandle string
	streamer        logstreamer.LogStreamer
	backendPlugin   backend_plugin.BackendPlugin
	wardenClient    gordon.Client
	logger          *steno.Logger
}

type RunActionTimeoutError struct {
	Action models.RunAction
}

func (e RunActionTimeoutError) Error() string {
	return fmt.Sprintf("action timed out after %s", e.Action.Timeout)
}

func New(
	model models.RunAction,
	containerHandle string,
	streamer logstreamer.LogStreamer,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *RunAction {
	return &RunAction{
		model:           model,
		containerHandle: containerHandle,
		streamer:        streamer,
		backendPlugin:   backendPlugin,
		wardenClient:    wardenClient,
		logger:          logger,
	}
}

func (action *RunAction) Perform(result chan error) {
	action.logger.Infod(
		map[string]interface{}{
			"handle": action.containerHandle,
		},
		"runonce.handle.run-action",
	)

	result <- action.perform()
}

func (action *RunAction) Cancel() {}

func (action *RunAction) Cleanup() {}

func (action *RunAction) perform() error {
	exitStatusChan := make(chan uint32, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if action.model.Timeout != 0 {
		timeoutChan = time.After(action.model.Timeout)
	}

	go func() {
		_, stream, err := action.wardenClient.Run(
			action.containerHandle,
			action.backendPlugin.BuildRunScript(action.model),
		)

		if err != nil {
			errChan <- err
			return
		}

		for payload := range stream {
			if payload.ExitStatus != nil {
				if action.streamer != nil {
					action.streamer.Flush()
				}

				exitStatusChan <- payload.GetExitStatus()
				break
			}

			if action.streamer != nil {
				switch *payload.Source {
				case warden.ProcessPayload_stdout:
					action.streamer.StreamStdout(payload.GetData())
				case warden.ProcessPayload_stderr:
					action.streamer.StreamStderr(payload.GetData())
				}
			}
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		if exitStatus != 0 {
			return fmt.Errorf("Process returned with exit value: %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return RunActionTimeoutError{Action: action.model}
	}

	panic("unreachable")
}
