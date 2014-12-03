package steps

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/lager"
)

type runStep struct {
	container            garden_api.Container
	model                models.RunAction
	streamer             log_streamer.LogStreamer
	logger               lager.Logger
	allowPrivileged      bool
	externalIP           string
	portMappings         []executor.PortMapping
	exportNetworkEnvVars bool
}

func NewRun(
	container garden_api.Container,
	model models.RunAction,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
	allowPrivileged bool,
	externalIP string,
	portMappings []executor.PortMapping,
	exportNetworkEnvVars bool,
) *runStep {
	logger = logger.Session("RunAction")
	return &runStep{
		container:            container,
		model:                model,
		streamer:             streamer,
		logger:               logger,
		allowPrivileged:      allowPrivileged,
		externalIP:           externalIP,
		portMappings:         portMappings,
		exportNetworkEnvVars: exportNetworkEnvVars,
	}
}

func (step *runStep) Perform() error {
	step.logger.Info("running")

	if step.model.Privileged && !step.allowPrivileged {
		step.logger.Info("privileged-action-denied")
		return errors.New("privileged-action-denied")
	}

	envVars := convertEnvironmentVariables(step.model.Env)

	if step.exportNetworkEnvVars {
		envVars = append(envVars, step.networkingEnvVars()...)
	}

	exitStatusChan := make(chan int, 1)
	errChan := make(chan error, 1)

	step.logger.Info("creating-process")
	process, err := step.container.Run(garden_api.ProcessSpec{
		Path:       step.model.Path,
		Args:       step.model.Args,
		Env:        envVars,
		Privileged: step.model.Privileged,

		Limits: garden_api.ResourceLimits{Nofile: step.model.ResourceLimits.Nofile},
	}, garden_api.ProcessIO{
		Stdout: step.streamer.Stdout(),
		Stderr: step.streamer.Stderr(),
	})
	if err != nil {
		step.logger.Error("failed-creating-process", err)
		return err
	}

	logger := step.logger.WithData(lager.Data{"process": process.ID()})
	logger.Info("successful-process-create")

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
		logger.Info("process-exit", lager.Data{"exitStatus": exitStatus})
		step.streamer.Stdout().Write([]byte(fmt.Sprintf("Exit status %d", exitStatus)))
		step.streamer.Flush()

		if exitStatus != 0 {
			info, err := step.container.Info()
			if err != nil {
				logger.Error("failed-to-get-info", err)
			} else {
				for _, ev := range info.Events {
					if ev == "out of memory" {
						return NewEmittableError(nil, "Exited with status %d (out of memory)", exitStatus)
					}
				}
			}

			return NewEmittableError(nil, "Exited with status %d", exitStatus)
		}

		return nil

	case err := <-errChan:
		logger.Error("running-error", err)
		return err
	}

	panic("unreachable")
}

func (step *runStep) Cancel() {
	step.container.Stop(false)
}

func convertEnvironmentVariables(environmentVariables []models.EnvironmentVariable) []string {
	converted := []string{}

	for _, env := range environmentVariables {
		converted = append(converted, env.Name+"="+env.Value)
	}

	return converted
}

func (step *runStep) networkingEnvVars() []string {
	var envVars []string

	envVars = append(envVars, "INSTANCE_IP="+step.externalIP)

	if len(step.portMappings) > 0 {
		envVars = append(envVars, fmt.Sprintf("INSTANCE_PORT=%d", step.portMappings[0].HostPort))
		envVars = append(envVars, fmt.Sprintf("INSTANCE_ADDR=%s:%d", step.externalIP, step.portMappings[0].HostPort))

		buffer := bytes.NewBufferString("INSTANCE_PORTS=")
		for i, portMapping := range step.portMappings {
			if i > 0 {
				buffer.WriteString(",")
			}
			buffer.WriteString(fmt.Sprintf("%d:%d", portMapping.HostPort, portMapping.ContainerPort))
		}
		envVars = append(envVars, buffer.String())
	} else {
		envVars = append(envVars, "INSTANCE_PORT=")
		envVars = append(envVars, "INSTANCE_ADDR=")
		envVars = append(envVars, "INSTANCE_PORTS=")
	}

	return envVars
}
