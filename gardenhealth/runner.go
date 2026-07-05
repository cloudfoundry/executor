package gardenhealth

import (
	"os"
	"time"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager/v3"
)

const (
	CellUnhealthyMetric           = "UnhealthyCell"
	GardenHealthCheckFailedMetric = "GardenHealthCheckFailed"

	// MaxInitialRetries is the number of times the initial healthcheck is
	// retried on timeout before declaring the cell fatally unhealthy. This
	// accommodates slow container runtimes (e.g. gVisor) where the first
	// healthcheck container creation can take longer than the configured
	// timeout due to one-time overlay filesystem setup.
	MaxInitialRetries = 3

	// initialRetryDelay is how long to wait after cancelling a timed-out
	// initial healthcheck before spawning the next attempt.
	initialRetryDelay = 5 * time.Second
)

type HealthcheckTimeoutError struct{}

func (HealthcheckTimeoutError) Error() string {
	return "garden healthcheck timed out"
}

type Runner struct {
	checkInterval    time.Duration
	timeoutInterval  time.Duration
	emissionInterval time.Duration
	logger           lager.Logger
	checker          Checker
	executorClient   executor.Client
	clock            clock.Clock
	metronClient     loggingclient.IngressClient
}

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
		timeoutInterval:  timeoutInterval,
		emissionInterval: emissionInterval,
		logger:           logger,
		checker:          checker,
		executorClient:   executorClient,
		clock:            clock,
		metronClient:     metronClient,
	}
}

// Once a healthcheck completes the runner will set the executor as healthy and
// close the ready channel. If the healthcheck does not complete within a
// timeout period, the runner will set the executor as unhealthy and the
// executor will not register itself with the BBS.
//
// The healthcheck is run periodically on an interval once the executor is
// healthy. If the periodic healthcheck fails, the executor is again set to
// unhealthy. A new healthcheck cycle will not be started until the previous one
// runs periodically.
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

	initialRetries := 0

	go r.healthcheckCycle(logger, healthcheckComplete)

	// Initial healthcheck phase — must pass once before we signal ready
	for {
		select {
		case signal := <-signals:
			logger.Info("signalled", lager.Data{"signal": signal.String()})
			return nil

		case <-healthcheckTimeout.C():
			r.setUnhealthy(logger)
			r.checker.Cancel(logger)

			initialRetries++
			if initialRetries > MaxInitialRetries {
				logger.Error("initial-healthcheck-exhausted-retries", nil, lager.Data{
					"retries": initialRetries - 1,
				})
				return HealthcheckTimeoutError{}
			}

			logger.Info("initial-healthcheck-timeout-retrying", lager.Data{
				"attempt": initialRetries,
				"max":     MaxInitialRetries,
			})

			// Brief pause to let garden clean up, then retry
			time.Sleep(initialRetryDelay)
			healthcheckTimeout.Reset(r.timeoutInterval)
			go r.healthcheckCycle(logger, healthcheckComplete)

		case err := <-healthcheckComplete:
			if err != nil {
				initialRetries++
				if initialRetries > MaxInitialRetries {
					logger.Error("initial-healthcheck-failed-exhausted-retries", err, lager.Data{
						"retries": initialRetries - 1,
					})
					r.setUnhealthy(logger)
					return err
				}

				logger.Error("initial-healthcheck-failed-retrying", err, lager.Data{
					"attempt": initialRetries,
					"max":     MaxInitialRetries,
				})

				time.Sleep(initialRetryDelay)
				healthcheckTimeout.Reset(r.timeoutInterval)
				go r.healthcheckCycle(logger, healthcheckComplete)
				continue
			}
			healthcheckTimeout.Stop()
		}

		// Only reach here on successful healthcheck (err == nil, timeout stopped)
		break
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
	r.executorClient.SetHealthy(logger, true)
	r.emitUnhealthyCellMetric(logger)
}

func (r *Runner) setUnhealthy(logger lager.Logger) {
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
