package monitor_step

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
)

type monitorStep struct {
	check sequence.Step

	interval           time.Duration
	healthyThreshold   uint
	unhealthyThreshold uint

	healthyHook   http.Request
	unhealthyHook http.Request

	cancel chan struct{}
}

func New(
	check sequence.Step,
	interval time.Duration,
	healthyThreshold, unhealthyThreshold uint,
	healthyHook, unhealthyHook http.Request,
) sequence.Step {
	return &monitorStep{
		check:              check,
		interval:           interval,
		healthyThreshold:   healthyThreshold,
		unhealthyThreshold: unhealthyThreshold,
		healthyHook:        healthyHook,
		unhealthyHook:      unhealthyHook,

		cancel: make(chan struct{}),
	}
}

func (step *monitorStep) Perform() error {
	timer := time.NewTicker(step.interval)

	var healthyCount uint
	var unhealthyCount uint

	for {
		select {
		case <-timer.C:
			healthy := step.check.Perform() == nil

			if healthy {
				healthyCount++
				unhealthyCount = 0
			} else {
				unhealthyCount++
				healthyCount = 0
			}

			var request *http.Request

			if healthyCount >= step.healthyThreshold {
				healthyCount = 0
				request = &step.healthyHook
			}

			if unhealthyCount >= step.unhealthyThreshold {
				unhealthyCount = 0
				request = &step.unhealthyHook
			}

			if request != nil {
				resp, err := http.DefaultClient.Do(request)
				if err == nil {
					resp.Body.Close()
				}
			}
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
