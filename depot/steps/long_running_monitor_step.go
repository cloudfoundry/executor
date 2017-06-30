package steps

import (
	"fmt"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/lager"
)

type longRunningMonitorStep struct {
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

func NewLongRunningMonitor(
	readinessCheck Step,
	livenessCheck Step,
	hasStartedRunning chan<- struct{},
	logger lager.Logger,
	clock clock.Clock,
	logStreamer log_streamer.LogStreamer,
	healthcheckStreamer log_streamer.LogStreamer,
	startTimeout time.Duration,
) Step {
	logger = logger.Session("monitor-step")

	return &longRunningMonitorStep{
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

func (step *longRunningMonitorStep) Perform() error {
	fmt.Fprint(step.logStreamer.Stdout(), "Starting health monitoring of container\n")

	errCh := make(chan error)

	go func() {
		errCh <- step.readinessCheck.Perform()
	}()

	var timerChan <-chan time.Time
	if step.startTimeout > 0 {
		timer := step.clock.NewTimer(step.startTimeout)
		timerChan = timer.C()
	} else {
		// do nothing, receiving from a nil channel should block forever
	}

	select {
	case err := <-errCh:
		if err != nil {
			return NewEmittableError(err, timeoutCrashReason, step.startTimeout, err.Error())
		}
	case <-timerChan:
		step.readinessCheck.Cancel()
		err := <-errCh
		errorString := err.Error()
		fmt.Fprintf(step.healthCheckStreamer.Stderr(), "%s\n", errorString)
		fmt.Fprintf(step.logStreamer.Stderr(), timeoutMessage, step.startTimeout)
		step.logger.Info("timed-out-before-healthy")
		return NewEmittableError(err, timeoutCrashReason, step.startTimeout, errorString)
	case <-step.cancelled:
		step.readinessCheck.Cancel()
		return <-errCh
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
		fmt.Fprint(step.logStreamer.Stdout(), "Container became unhealthy\n")
		errorString := err.Error()
		fmt.Fprintf(step.healthCheckStreamer.Stderr(), "%s\n", errorString)
		return NewEmittableError(err, healthcheckNowUnhealthy, errorString)
	case <-step.cancelled:
		step.livenessCheck.Cancel()
		return <-errCh
	}
}
