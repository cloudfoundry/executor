package gardenhealth

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type Runner struct {
	interval       time.Duration
	logger         lager.Logger
	checker        Checker
	executorClient executor.Client
	clock          clock.Clock
}

func NewRunner(
	interval time.Duration,
	logger lager.Logger,
	checker Checker,
	executorClient executor.Client,
	clock clock.Clock,
) *Runner {
	return &Runner{
		interval:       interval,
		logger:         logger.Session("garden-health-check"),
		checker:        checker,
		executorClient: executorClient,
		clock:          clock,
	}
}

func (r *Runner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	var (
		checkResult                       = make(chan error, 1)
		startHealthcheck <-chan time.Time = r.clock.NewTimer(0).C()
		failures                          = 0
		healthy                           = true
	)

	close(ready)

	r.logger.Info("starting")

	for {
		select {
		case <-signals:
			r.logger.Info("complete")
			return nil

		case <-startHealthcheck:
			r.logger.Info("check-starting")
			startHealthcheck = nil
			go func() {
				checkResult <- r.checker.Healthcheck(r.logger)
			}()

		case err := <-checkResult:
			switch err.(type) {
			case nil:
				r.logger.Info("passed-health-check")
				failures = 0
				if !healthy {
					r.logger.Info("set-state-healthy")
					r.executorClient.SetHealthy(true)
					healthy = true
				}

			case UnrecoverableError:
				r.logger.Error("failed-unrecoverable-error", err)
				return err

			default:
				r.logger.Info("failed-health-check")
				failures++
				if failures >= 3 {
					r.logger.Error("set-state-unhealthy", nil)
					r.executorClient.SetHealthy(false)
					healthy = false
				}
			}

			startHealthcheck = r.clock.NewTimer(r.interval).C()

			r.logger.Info("check-complete")
		}
	}

	return nil
}
