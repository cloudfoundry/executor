package run_step

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
)

type RunStep struct {
	container           warden.Container
	model               models.RunAction
	fileDescriptorLimit uint64
	streamer            log_streamer.LogStreamer
	logger              *steno.Logger
}

func New(
	container warden.Container,
	model models.RunAction,
	fileDescriptorLimit uint64,
	streamer log_streamer.LogStreamer,
	logger *steno.Logger,
) *RunStep {
	return &RunStep{
		container:           container,
		model:               model,
		fileDescriptorLimit: fileDescriptorLimit,
		streamer:            streamer,
		logger:              logger,
	}
}

func convertEnvironmentVariables(environmentVariables []models.EnvironmentVariable) []warden.EnvironmentVariable {
	convertedEnvironmentVariables := []warden.EnvironmentVariable{}

	for _, env := range environmentVariables {
		convertedEnvironmentVariables = append(convertedEnvironmentVariables, warden.EnvironmentVariable{env.Key, env.Value})
	}

	return convertedEnvironmentVariables
}

func (step *RunStep) Perform() error {
	step.logger.Debugd(
		map[string]interface{}{
			"handle": step.container.Handle(),
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
		var nofile *uint64
		if step.fileDescriptorLimit != 0 {
			nofile = &step.fileDescriptorLimit
		}

		_, stream, err := step.container.Run(warden.ProcessSpec{
			Script: step.model.Script,
			Limits: warden.ResourceLimits{Nofile: nofile},

			EnvironmentVariables: convertEnvironmentVariables(step.model.Env),
		})

		if err != nil {
			errChan <- err
			return
		}

		for payload := range stream {
			if payload.ExitStatus != nil {
				step.streamer.Flush()

				exitStatusChan <- *payload.ExitStatus
				break
			}

			switch payload.Source {
			case warden.ProcessStreamSourceStdout:
				fmt.Fprint(step.streamer.Stdout(), string(payload.Data))
			case warden.ProcessStreamSourceStderr:
				fmt.Fprint(step.streamer.Stderr(), string(payload.Data))
			}
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		info, err := step.container.Info()
		if err != nil {
			step.logger.Errord(
				map[string]interface{}{
					"handle": step.container.Handle(),
					"err":    err.Error(),
				},
				"run-step.info.failed",
			)
		} else {
			for _, ev := range info.Events {
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
	step.container.Stop(false)
}

func (step *RunStep) Cleanup() {}
