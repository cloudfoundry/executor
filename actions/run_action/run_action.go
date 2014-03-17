package run_action

import (
	"errors"
	"fmt"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
	"github.com/vito/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type RunAction struct {
	containerHandle     string
	model               models.RunAction
	fileDescriptorLimit int
	streamer            log_streamer.LogStreamer
	backendPlugin       backend_plugin.BackendPlugin
	wardenClient        gordon.Client
	logger              *steno.Logger
}

type TimeoutError struct {
	Action models.RunAction
}

func (e TimeoutError) Error() string {
	return fmt.Sprintf("timed out after %s", e.Action.Timeout)
}

var OOMError = errors.New("out of memory")

func New(
	containerHandle string,
	model models.RunAction,
	fileDescriptorLimit int,
	streamer log_streamer.LogStreamer,
	backendPlugin backend_plugin.BackendPlugin,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *RunAction {
	return &RunAction{
		containerHandle:     containerHandle,
		model:               model,
		fileDescriptorLimit: fileDescriptorLimit,
		streamer:            streamer,
		backendPlugin:       backendPlugin,
		wardenClient:        wardenClient,
		logger:              logger,
	}
}

func (action *RunAction) Perform() error {
	action.logger.Debugd(
		map[string]interface{}{
			"handle": action.containerHandle,
		},
		"run-action.perform",
	)

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
			gordon.ResourceLimits{
				FileDescriptors: uint64(action.fileDescriptorLimit),
			},
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
		info, err := action.wardenClient.Info(action.containerHandle)
		if err != nil {
			action.logger.Errord(
				map[string]interface{}{
					"handle": action.containerHandle,
					"err":    err.Error(),
				},
				"run-action.info.failed",
			)
		} else {
			for _, ev := range info.GetEvents() {
				if ev == "out of memory" {
					return OOMError
				}
			}
		}

		if exitStatus != 0 {
			return fmt.Errorf("Process returned with exit value: %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return TimeoutError{Action: action.model}
	}

	panic("unreachable")
}

func (action *RunAction) Cancel() {
	action.wardenClient.Stop(action.containerHandle, false, false)
}

func (action *RunAction) Cleanup() {}
