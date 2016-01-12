package gardenhealth

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

const unhealthyCell = metric.Metric("UnhealthyCell")

type HealthcheckTimeoutError struct{}

func (HealthcheckTimeoutError) Error() string {
	return "garden healthcheck timed out"
}

//go:generate counterfeiter -o fakegardenhealth/fake_timerprovider.go . TimerProvider

type TimerProvider interface {
	NewTimer(time.Duration) clock.Timer
}

// Runner coordinates health checks against an executor client.  When checks fail or
// time out, its executor will be marked as unhealthy until a successful check occurs.
//
// See NewRunner and Runner.Run for more details.
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

// NewRunner constructs a healthcheck runner.
//
// The checkInterval parameter controls how often the healthcheck should run, and
// the timeoutInterval sets the time to wait for the healthcheck to complete before
// marking the executor as unhealthy.
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

// Run coordinates the execution of the healthcheck. It responds to incoming signals,
// monitors the elapsed time to determine timeouts, and ensures the healthcheck runs periodically.
//
// Note: If the healthcheck has not returned before the timeout expires, we
// intentionally do not kill the healthcheck process, and we don't spawn a new healthcheck
// until the existing healthcheck exits. It may be necessary for an operator to
// inspect the long running container to debug the problem.
func (r *Runner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	var (
		startHealthcheck    = r.timerProvider.NewTimer(0)
		healthcheckTimeout  = r.timerProvider.NewTimer(r.timeoutInterval)
		healthcheckComplete = make(chan error, 1)
	)
	r.logger.Info("starting")

	go r.healthcheckCycle(healthcheckComplete)

	select {
	case <-signals:
		return nil

	case <-healthcheckTimeout.C():
		r.logger.Error("failed-initial-healthcheck-timeout", nil)
		r.setUnhealthy()
		return HealthcheckTimeoutError{}

	case err := <-healthcheckComplete:
		if err != nil {
			r.logger.Error("failed-initial-healthcheck", err)
			r.setUnhealthy()
			return err
		}
		healthcheckTimeout.Stop()
	}

	r.logger.Info("passed-initial-healthcheck")
	r.setHealthy()

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
			go r.healthcheckCycle(healthcheckComplete)
			healthcheckTimeout.Reset(r.timeoutInterval)

		case <-healthcheckTimeout.C():
			r.logger.Error("failed-healthcheck-timeout", nil)
			r.setUnhealthy()

		case err := <-healthcheckComplete:
			timeoutOk := healthcheckTimeout.Stop()
			switch err.(type) {
			case nil:
				r.logger.Info("passed-health-check")
				if timeoutOk {
					r.setHealthy()
				}

			default:
				r.logger.Error("failed-health-check", err)
				r.setUnhealthy()
			}

			startHealthcheck.Reset(r.checkInterval)
			r.logger.Info("check-complete")
		}
	}
}

func (r *Runner) setHealthy() {
	err := unhealthyCell.Send(0)
	if err != nil {
		r.logger.Error("failed-to-send-unhealthy-cell-metric", err)
	}
	if !r.healthy {
		r.logger.Info("set-state-healthy")
		r.executorClient.SetHealthy(r.logger, true)
		r.healthy = true
	}
}

func (r *Runner) setUnhealthy() {
	err := unhealthyCell.Send(1)
	if err != nil {
		r.logger.Error("failed-to-send-unhealthy-cell-metric", err)
	}
	if r.healthy {
		r.logger.Error("set-state-unhealthy", nil)
		r.executorClient.SetHealthy(r.logger, false)
		r.healthy = false
	}
}

func (r *Runner) healthcheckCycle(healthcheckComplete chan<- error) {
	healthcheckComplete <- r.checker.Healthcheck(r.logger)
}
