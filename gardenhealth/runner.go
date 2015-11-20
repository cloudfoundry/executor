package gardenhealth

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type HealthcheckTimeoutError struct{}

func (HealthcheckTimeoutError) Error() string {
	return "garden healthcheck timed out"
}

//go:generate counterfeiter -o fakegardenhealth/fake_timerprovider.go . TimerProvider

type TimerProvider interface {
	NewTimer(time.Duration) clock.Timer
}

type Runner struct {
	failures        int
	healthy         bool
	checkInterval   time.Duration
	timeoutInterval time.Duration
	logger          lager.Logger
	checker         Checker
	executorClient  executor.Client
	timerProvider   TimerProvider
}

func NewRunner(
	checkInterval time.Duration,
	timeoutInterval time.Duration,
	logger lager.Logger,
	checker Checker,
	executorClient executor.Client,
	timerProvider TimerProvider,
) *Runner {
	return &Runner{
		checkInterval:   checkInterval,
		timeoutInterval: timeoutInterval,
		logger:          logger.Session("garden-healthcheck"),
		checker:         checker,
		executorClient:  executorClient,
		timerProvider:   timerProvider,
		healthy:         false,
		failures:        0,
	}
}

func (r *Runner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	var (
		startHealthcheck    = r.timerProvider.NewTimer(0)
		healthcheckTimeout  = r.timerProvider.NewTimer(r.timeoutInterval)
		healthcheckComplete = make(chan error, 1)
	)
	r.logger.Info("starting")

	go r.HealthcheckCycle(healthcheckComplete)

	select {
	case <-signals:
		return nil

	case <-healthcheckTimeout.C():
		r.logger.Error("failed-initial-healthcheck-timeout", nil)
		return HealthcheckTimeoutError{}

	case err := <-healthcheckComplete:
		if err != nil {
			r.logger.Error("failed-initial-healthcheck", err)
			return err
		}
		healthcheckTimeout.Stop()
	}

	r.logger.Info("passed-initial-healthcheck")
	r.SetHealthy()

	close(ready)
	r.logger.Info("started")

	startHealthcheck.Reset(r.checkInterval)

	for {
		select {
		case <-signals:
			r.logger.Info("complete")
			return nil

		case <-startHealthcheck.C():
			r.logger.Info("check-starting")
			go r.HealthcheckCycle(healthcheckComplete)
			healthcheckTimeout.Reset(r.timeoutInterval)

		case <-healthcheckTimeout.C():
			r.logger.Error("failed-healthcheck-timeout", nil)
			r.SetUnhealthy()

		case err := <-healthcheckComplete:
			timeoutOk := healthcheckTimeout.Stop()
			switch err.(type) {
			case nil:
				r.logger.Info("passed-health-check")
				if timeoutOk {
					r.SetHealthy()
				}

			default:
				r.logger.Error("failed-health-check", err)
				r.SetUnhealthy()
			}

			startHealthcheck.Reset(r.checkInterval)
			r.logger.Info("check-complete")
		}
	}
}

func (r *Runner) SetHealthy() {
	if !r.healthy {
		r.logger.Info("set-state-healthy")
		r.executorClient.SetHealthy(true)
		r.healthy = true
	}
}

func (r *Runner) SetUnhealthy() {
	if r.healthy {
		r.logger.Error("set-state-unhealthy", nil)
		r.executorClient.SetHealthy(false)
		r.healthy = false
	}
}

func (r *Runner) HealthcheckCycle(healthcheckComplete chan<- error) {
	healthcheckComplete <- r.checker.Healthcheck(r.logger)
}
