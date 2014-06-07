package monitor_step

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
	steno "github.com/cloudfoundry/gosteno"
)

const BaseInterval = 500 * time.Millisecond
const MaxInterval = 30 * time.Second

type monitorStep struct {
	check sequence.Step

	healthyThreshold   uint
	unhealthyThreshold uint

	healthyHook   *http.Request
	unhealthyHook *http.Request

	logger *steno.Logger

	cancel chan struct{}
}

func New(
	check sequence.Step,
	healthyThreshold, unhealthyThreshold uint,
	healthyHook, unhealthyHook *http.Request,
	logger *steno.Logger,
) sequence.Step {
	if healthyThreshold == 0 {
		healthyThreshold = 1
	}

	if unhealthyThreshold == 0 {
		unhealthyThreshold = 1
	}

	return &monitorStep{
		check:              check,
		healthyThreshold:   healthyThreshold,
		unhealthyThreshold: unhealthyThreshold,
		healthyHook:        healthyHook,
		unhealthyHook:      unhealthyHook,
		logger:             logger,

		cancel: make(chan struct{}),
	}
}

func (step *monitorStep) Perform() error {
	timer := time.After(BaseInterval)

	var healthyCount uint
	var unhealthyCount uint

	for {
		select {
		case <-timer:
			healthy := step.check.Perform() == nil

			var request *http.Request

			if healthy {
				healthyCount++
				unhealthyCount = 0

				if step.healthyHook != nil && (healthyCount%step.healthyThreshold) == 0 {
					request = step.healthyHook
				}

				step.logger.Debugd(map[string]interface{}{
					"healthy-count": healthyCount,
				}, "executor.health-monitor.healthy")
			} else {
				unhealthyCount++
				healthyCount = 0

				if step.unhealthyHook != nil && (unhealthyCount%step.unhealthyThreshold) == 0 {
					request = step.unhealthyHook
				}

				step.logger.Debugd(map[string]interface{}{
					"unhealthy-count": unhealthyCount,
				}, "executor.health-monitor.unhealthy")
			}

			if request != nil {
				resp, err := http.DefaultClient.Do(request)
				if err != nil {
					step.logger.Errord(map[string]interface{}{
						"error": err,
					}, "executor.health-monitor.request-failed")
				} else {
					resp.Body.Close()
				}
			}

			backoff := BaseInterval * time.Duration(1<<(healthyCount-1))
			if backoff < BaseInterval {
				backoff = BaseInterval
			} else if backoff > MaxInterval {
				backoff = MaxInterval
			}

			step.logger.Debugd(map[string]interface{}{
				"duration": backoff,
			}, "executor.health-monitor.sleeping")

			timer = time.After(backoff)
		case <-step.cancel:
			return nil
		}
	}

	return nil
}

func (step *monitorStep) Cancel() {
	step.cancel <- struct{}{}
}

func (step *monitorStep) Cleanup() {
}
