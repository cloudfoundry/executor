package monitor_step

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/timer"
)

const BaseInterval = 500 * time.Millisecond
const MaxInterval = 30 * time.Second

type monitorStep struct {
	check sequence.Step

	healthyThreshold   uint
	unhealthyThreshold uint

	healthyHook   *http.Request
	unhealthyHook *http.Request

	logger lager.Logger
	timer  timer.Timer

	cancel chan struct{}
}

func New(
	check sequence.Step,
	healthyThreshold, unhealthyThreshold uint,
	healthyHook, unhealthyHook *http.Request,
	logger lager.Logger,
	timer timer.Timer,
) sequence.Step {
	if healthyThreshold == 0 {
		healthyThreshold = 1
	}

	if unhealthyThreshold == 0 {
		unhealthyThreshold = 1
	}

	logger = logger.Session("MonitorAction")
	return &monitorStep{
		check:              check,
		healthyThreshold:   healthyThreshold,
		unhealthyThreshold: unhealthyThreshold,
		healthyHook:        healthyHook,
		unhealthyHook:      unhealthyHook,
		logger:             logger,
		timer:              timer,

		cancel: make(chan struct{}),
	}
}

func (step *monitorStep) Perform() error {
	timer := step.timer.After(BaseInterval)

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

				step.logger.Debug("healthy", lager.Data{
					"healthy-count": healthyCount,
				})
			} else {
				unhealthyCount++
				healthyCount = 0

				if step.unhealthyHook != nil && (unhealthyCount%step.unhealthyThreshold) == 0 {
					request = step.unhealthyHook
				}

				step.logger.Info("unhealthy", lager.Data{
					"unhealthy-count": unhealthyCount,
				})
			}

			if request != nil {
				resp, err := http.DefaultClient.Do(request)
				if err != nil {
					step.logger.Error("callback-failed", err)
				} else {
					resp.Body.Close()
				}
			}

			backoff := backoffForHealthyCount(healthyCount)
			step.logger.Debug("sleeping", lager.Data{
				"duration": backoff,
			})
			timer = step.timer.After(backoff)
		case <-step.cancel:
			return nil
		}
	}

	return nil
}

func backoffForHealthyCount(healthyCount uint) time.Duration {
	if healthyCount == 0 {
		return BaseInterval
	}

	backoff := BaseInterval * time.Duration(uint(1)<<(healthyCount-1))

	// Guard against integer overflow caused by bitshifting
	if backoff > MaxInterval || healthyCount > 32 {
		return MaxInterval
	} else {
		return backoff
	}
}

func (step *monitorStep) Cancel() {
	step.cancel <- struct{}{}
}
