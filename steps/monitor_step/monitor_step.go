package monitor_step

import (
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/sequence"
)

const BaseInterval = 500 * time.Millisecond
const MaxInterval = 30 * time.Second

type monitorStep struct {
	check sequence.Step

	healthyThreshold   uint
	unhealthyThreshold uint

	healthyHook   *http.Request
	unhealthyHook *http.Request

	cancel chan struct{}
}

func New(
	check sequence.Step,
	healthyThreshold, unhealthyThreshold uint,
	healthyHook, unhealthyHook *http.Request,
) sequence.Step {
	return &monitorStep{
		check:              check,
		healthyThreshold:   healthyThreshold,
		unhealthyThreshold: unhealthyThreshold,
		healthyHook:        healthyHook,
		unhealthyHook:      unhealthyHook,

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
			} else {
				unhealthyCount++
				healthyCount = 0

				if step.unhealthyHook != nil && (unhealthyCount%step.unhealthyThreshold) == 0 {
					request = step.unhealthyHook
				}
			}

			if request != nil {
				resp, err := http.DefaultClient.Do(request)
				if err == nil {
					resp.Body.Close()
				}
			}

			backoff := BaseInterval * time.Duration(1<<(healthyCount-1))
			if backoff < BaseInterval {
				backoff = BaseInterval
			} else if backoff > MaxInterval {
				backoff = MaxInterval
			}

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
