package run_step

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/gordon/warden"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type RunStep struct {
	containerHandle     string
	model               models.RunAction
	fileDescriptorLimit int
	streamer            log_streamer.LogStreamer
	wardenClient        gordon.Client
	logger              *steno.Logger
}

func New(
	containerHandle string,
	model models.RunAction,
	fileDescriptorLimit int,
	streamer log_streamer.LogStreamer,
	wardenClient gordon.Client,
	logger *steno.Logger,
) *RunStep {
	return &RunStep{
		containerHandle:     containerHandle,
		model:               model,
		fileDescriptorLimit: fileDescriptorLimit,
		streamer:            streamer,
		wardenClient:        wardenClient,
		logger:              logger,
	}
}

func convertEnvironmentVariables(environmentVariables []models.EnvironmentVariable) []gordon.EnvironmentVariable {
	convertedEnvironmentVariables := []gordon.EnvironmentVariable{}

	for _, env := range environmentVariables {
		convertedEnvironmentVariables = append(convertedEnvironmentVariables, gordon.EnvironmentVariable{env.Key, env.Value})
	}

	return convertedEnvironmentVariables
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
		timer := time.NewTimer(step.model.Timeout)
		timeoutChan = timer.C
		defer timer.Stop()
	}

	go func() {
		_, stream, err := step.wardenClient.Run(
			step.containerHandle,
			step.model.Script,
			gordon.ResourceLimits{
				FileDescriptors: uint64(step.fileDescriptorLimit),
			},
			convertEnvironmentVariables(step.model.Env),
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
					return emittable_error.New(nil, "Exited with status %d (out of memory)", exitStatus)
				}
			}
		}

		if exitStatus != 0 {
			return emittable_error.New(nil, "Exited with status %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		return err

	case <-timeoutChan:
		return emittable_error.New(nil, "Timed out after %s", step.model.Timeout)
	}

	panic("unreachable")
}

func (step *RunStep) Cancel() {
	step.wardenClient.Stop(step.containerHandle, false, false)
}

func (step *RunStep) Cleanup() {}
