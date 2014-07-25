package run_step

import (
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/lager"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
)

type RunStep struct {
	container warden.Container
	model     models.RunAction
	streamer  log_streamer.LogStreamer
	logger    lager.Logger
}

func New(
	container warden.Container,
	model models.RunAction,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
) *RunStep {
	return &RunStep{
		container: container,
		model:     model,
		streamer:  streamer,
		logger:    logger,
	}
}

func convertEnvironmentVariables(environmentVariables []models.EnvironmentVariable) []string {
	converted := []string{}

	for _, env := range environmentVariables {
		converted = append(converted, env.Name+"="+env.Value)
	}

	return converted
}

func (step *RunStep) Perform() error {
	step.logger.Debug("running")

	exitStatusChan := make(chan int, 1)
	errChan := make(chan error, 1)

	var timeoutChan <-chan time.Time

	if step.model.Timeout != 0 {
		timer := time.NewTimer(step.model.Timeout)
		timeoutChan = timer.C
		defer timer.Stop()
	}

	process, err := step.container.Run(warden.ProcessSpec{
		Path: step.model.Path,
		Args: step.model.Args,
		Env:  convertEnvironmentVariables(step.model.Env),

		Limits: warden.ResourceLimits{Nofile: step.model.ResourceLimits.Nofile},
	}, warden.ProcessIO{
		Stdout: step.streamer.Stdout(),
		Stderr: step.streamer.Stderr(),
	})
	if err != nil {
		return err
	}

	go func() {
		exitStatus, err := process.Wait()
		if err != nil {
			errChan <- err
		} else {
			exitStatusChan <- exitStatus
		}
	}()

	select {
	case exitStatus := <-exitStatusChan:
		step.streamer.Flush()

		info, err := step.container.Info()
		if err != nil {
			step.logger.Error("failed-to-get-info", err)
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
