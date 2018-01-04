package steps

import (
	"fmt"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/lager"
)

const (
	timeoutMessage          = "Timed out after %s: health check never passed.\n"
	timeoutCrashReason      = "Instance never healthy after %s: %s"
	healthcheckNowUnhealthy = "Instance became unhealthy: %s"
)

type healthCheckStep struct {
	hasStartedRunning chan<- struct{}

	readinessCheck Step
	livenessCheck  Step

	logger              lager.Logger
	clock               clock.Clock
	logStreamer         log_streamer.LogStreamer
	healthCheckStreamer log_streamer.LogStreamer

	startTimeout time.Duration

	*canceller
}

func NewHealthCheckStep(
	readinessCheck Step,
	livenessCheck Step,
	hasStartedRunning chan<- struct{},
	logger lager.Logger,
	clock clock.Clock,
	logStreamer log_streamer.LogStreamer,
	healthcheckStreamer log_streamer.LogStreamer,
	startTimeout time.Duration,
) Step {
	logger = logger.Session("health-check-step")

	return &healthCheckStep{
		readinessCheck:      readinessCheck,
		livenessCheck:       livenessCheck,
		hasStartedRunning:   hasStartedRunning,
		logger:              logger,
		clock:               clock,
		logStreamer:         logStreamer,
		healthCheckStreamer: healthcheckStreamer,
		startTimeout:        startTimeout,
		canceller:           newCanceller(),
	}
}

func (step *healthCheckStep) Perform() error {
	fmt.Fprint(step.logStreamer.Stdout(), "Starting health monitoring of container\n")

	errCh := make(chan error)

	go func() {
		errCh <- step.readinessCheck.Perform()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(step.healthCheckStreamer.Stderr(), "%s\n", err.Error())
			fmt.Fprintf(step.logStreamer.Stderr(), timeoutMessage, step.startTimeout)
			step.logger.Info("timed-out-before-healthy", lager.Data{
				"step-error": err.Error(),
			})
			return NewEmittableError(err, timeoutCrashReason, step.startTimeout, err.Error())
		}
	case <-step.cancelled:
		step.readinessCheck.Cancel()
		<-errCh
		return ErrCancelled
	}

	step.logger.Info("transitioned-to-healthy")
	fmt.Fprint(step.logStreamer.Stdout(), "Container became healthy\n")
	step.hasStartedRunning <- struct{}{}

	go func() {
		errCh <- step.livenessCheck.Perform()
	}()

	select {
	case err := <-errCh:
		step.logger.Info("transitioned-to-unhealthy")
		fmt.Fprintf(step.healthCheckStreamer.Stderr(), "%s\n", err.Error())
		fmt.Fprint(step.logStreamer.Stdout(), "Container became unhealthy\n")
		return NewEmittableError(err, healthcheckNowUnhealthy, err.Error())
	case <-step.cancelled:
		step.livenessCheck.Cancel()
		<-errCh
		return ErrCancelled
	}
}
