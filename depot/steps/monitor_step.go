package steps

import (
	"time"


	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/timer"
)

type HealthEvent bool

const (
	Healthy   HealthEvent = true
	Unhealthy HealthEvent = false
)

type monitorStep struct {
	check  Step
	events chan<- HealthEvent

	logger lager.Logger
	timer  timer.Timer

	healthyInterval   time.Duration
	unhealthyInterval time.Duration

	cancel chan struct{}
}

func NewMonitor(
	check Step,
	events chan<- HealthEvent,
	logger lager.Logger,
	timer timer.Timer,
	healthyInterval time.Duration,
	unhealthyInterval time.Duration,
) Step {
	logger = logger.Session("MonitorAction")

	if healthyInterval == 0 {
		panic("healthyInterval must be a positive value. Stay positive people!")
	}

	if unhealthyInterval == 0 {
		panic("unhealthyInterval must be a positive value. Stay positive people!")
	}

	return &monitorStep{
		check:             check,
		events:            events,
		logger:            logger,
		timer:             timer,
		healthyInterval:   healthyInterval,
		unhealthyInterval: unhealthyInterval,
		cancel:            make(chan struct{}),
	}
}

func (step *monitorStep) Perform() error {
	healthy := false
	interval := step.unhealthyInterval

	for {
		timer := step.timer.After(interval)

		select {
		case <-timer:
			stepErr := step.check.Perform()
			nowHealthy := stepErr == nil

			if healthy && !nowHealthy {
				step.events <- Unhealthy
				return stepErr
			} else if !healthy && nowHealthy {
				healthy = true
				step.events <- Healthy
				interval = step.healthyInterval
			}

		case <-step.cancel:
			return nil
		}
	}

	return nil
}

func (step *monitorStep) Cancel() {
	close(step.cancel)
}

func (step *monitorStep) Cleanup() {
}
