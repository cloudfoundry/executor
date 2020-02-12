package gardenhealth

import (
	"os"
	"time"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

const GardenHealthCheckFailedMetric = "GardenHealthCheckFailed"
const CellUnhealthyMetric = "CellUnhealthy"

type HealthcheckTimeoutError struct{}

func (HealthcheckTimeoutError) Error() string {
	return "garden healthcheck timed out"
}

// Runner coordinates health checks against an executor client.  When checks fail or
// time out, its executor will be marked as unhealthy until a successful check occurs.
//
// See NewRunner and Runner.Run for more details.
type Runner struct {
	failures         int
	healthy          bool
	checkInterval    time.Duration
	emissionInterval time.Duration
	timeoutInterval  time.Duration
	logger           lager.Logger
	checker          Checker
	executorClient   executor.Client
	metronClient     loggingclient.IngressClient
	clock            clock.Clock
}

// NewRunner constructs a healthcheck runner.
//
// The checkInterval parameter controls how often the healthcheck should run, and
// the timeoutInterval sets the time to wait for the healthcheck to complete before
// marking the executor as unhealthy.
func NewRunner(
	checkInterval time.Duration,
	emissionInterval time.Duration,
	timeoutInterval time.Duration,
	logger lager.Logger,
	checker Checker,
	executorClient executor.Client,
	metronClient loggingclient.IngressClient,
	clock clock.Clock,
) *Runner {
	return &Runner{
		checkInterval:    checkInterval,
		emissionInterval: emissionInterval,
		timeoutInterval:  timeoutInterval,
		logger:           logger.Session("garden-healthcheck"),
		checker:          checker,
		executorClient:   executorClient,
		metronClient:     metronClient,
		clock:            clock,
		healthy:          false,
		failures:         0,
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
	logger := r.logger.Session("garden-health")
	healthcheckTimeout := r.clock.NewTimer(r.timeoutInterval)
	healthcheckComplete := make(chan error, 1)

	logger.Info("starting")

	go r.healthcheckCycle(logger, healthcheckComplete)

	select {
	case signal := <-signals:
		logger.Info("signalled", lager.Data{"signal": signal.String()})
		return nil

	case <-healthcheckTimeout.C():
		r.setUnhealthy(logger)
		r.checker.Cancel(logger)
		logger.Info("timed-out")
		return HealthcheckTimeoutError{}

	case err := <-healthcheckComplete:
		if err != nil {
			r.setUnhealthy(logger)
			return err
		}
		healthcheckTimeout.Stop()
	}

	r.setHealthy(logger)

	close(ready)
	logger.Info("started")

	startHealthcheck := r.clock.NewTimer(r.checkInterval)
	emitInterval := r.clock.NewTicker(r.emissionInterval)
	defer emitInterval.Stop()

	for {
		select {
		case signal := <-signals:
			logger.Info("signalled-complete", lager.Data{"signal": signal.String()})
			return nil

		case <-startHealthcheck.C():
			healthcheckTimeout.Reset(r.timeoutInterval)
			go r.healthcheckCycle(logger, healthcheckComplete)

		case <-healthcheckTimeout.C():
			r.setUnhealthy(logger)
			r.checker.Cancel(logger)
			r.metronClient.SendMetric(CellUnhealthyMetric, 1)

		case <-emitInterval.C():
			r.emitUnhealthyCellMetric(logger)

		case err := <-healthcheckComplete:
			timeoutOk := healthcheckTimeout.Stop()
			switch err.(type) {
			case nil:
				if timeoutOk {
					r.setHealthy(logger)
				}

			default:
				r.setUnhealthy(logger)
			}

			startHealthcheck.Reset(r.checkInterval)
		}
	}
}

func (r *Runner) setHealthy(logger lager.Logger) {
	r.logger.Info("set-state-healthy")
	r.executorClient.SetHealthy(logger, true)
	r.emitUnhealthyCellMetric(logger)
}

func (r *Runner) setUnhealthy(logger lager.Logger) {
	r.logger.Error("set-state-unhealthy", nil)
	r.executorClient.SetHealthy(logger, false)
	r.emitUnhealthyCellMetric(logger)
}

func (r *Runner) emitUnhealthyCellMetric(logger lager.Logger) {
	var err error
	if r.executorClient.Healthy(logger) {
		err = r.metronClient.SendMetric(GardenHealthCheckFailedMetric, 0)
	} else {
		err = r.metronClient.SendMetric(GardenHealthCheckFailedMetric, 1)
	}

	if err != nil {
		logger.Error("failed-to-send-unhealthy-cell-metric", err)
	}
}

func (r *Runner) healthcheckCycle(logger lager.Logger, healthcheckComplete chan<- error) {
	healthcheckComplete <- r.checker.Healthcheck(logger)
}
