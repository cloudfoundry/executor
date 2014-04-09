package run_step

import (
	"errors"
	"fmt"
	"time"

	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/gordon/warden"

	"github.com/cloudfoundry-incubator/executor/backend_plugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type RunStep struct {
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
) *RunStep {
	return &RunStep{
		containerHandle:     containerHandle,
		model:               model,
		fileDescriptorLimit: fileDescriptorLimit,
		streamer:            streamer,
		backendPlugin:       backendPlugin,
		wardenClient:        wardenClient,
		logger:              logger,
	}
}

func (step *RunStep) Perform() error {
	step.logger.Debugd(
		map[string]interface{}{
			"handle": step.containerHandle,
		},
		"run-step.perform",
	)

	exitStatusChan := make(chan uint32, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if step.model.Timeout != 0 {
		timeoutChan = time.After(step.model.Timeout)
	}

	go func() {
		_, stream, err := step.wardenClient.Run(
			step.containerHandle,
			step.backendPlugin.BuildRunScript(step.model),
			gordon.ResourceLimits{
				FileDescriptors: uint64(step.fileDescriptorLimit),
			},
		)

		if err != nil {
			errChan <- err
			return
		}

		for payload := range stream {
			if payload.ExitStatus != nil {
				step.streamer.Flush()

				exitStatusChan <- payload.GetExitStatus()
				break
			}

			switch *payload.Source {
			case warden.ProcessPayload_stdout:
				fmt.Fprint(step.streamer.Stdout(), payload.GetData())
			case warden.ProcessPayload_stderr:
				fmt.Fprint(step.streamer.Stderr(), payload.GetData())
			}
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		info, err := step.wardenClient.Info(step.containerHandle)
		if err != nil {
			step.logger.Errord(
				map[string]interface{}{
					"handle": step.containerHandle,
					"err":    err.Error(),
				},
				"run-step.info.failed",
			)
		} else {
			for _, ev := range info.GetEvents() {
				if ev == "out of memory" {
					fmt.Fprintf(step.streamer.Stderr(), "%s exited with status %d (out of memory)\n", step.model.Name, exitStatus)
					return OOMError
				}
			}
		}

		if exitStatus != 0 {
			return fmt.Errorf("process exited with status %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return TimeoutError{Action: step.model}
	}

	panic("unreachable")
}

func (step *RunStep) Cancel() {
	step.wardenClient.Stop(step.containerHandle, false, false)
}

func (step *RunStep) Cleanup() {}
