package timeout_step

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/executor/depot/sequence"
)

type TimeoutError struct {
	SubstepError error
}

func (te TimeoutError) Error() string {
	return "Step sequence exceeded timeout; " + cancelledSubstepErrorMessage(te.SubstepError)
}

type CancelError struct {
	SubstepError error
}

func (ce CancelError) Error() string {
	return "Step was cancelled; " + cancelledSubstepErrorMessage(ce.SubstepError)
}

func cancelledSubstepErrorMessage(err error) string {
	if err == nil {
		return "cancelled substep did not return error"
	}

	return fmt.Sprintf("cancelled substep error: %s", err)
}

type TimeoutStep struct {
	substeps  []sequence.Step
	timeout   time.Duration
	cancelled chan struct{}
}

func New(substeps []sequence.Step, timeout time.Duration) *TimeoutStep {
	return &TimeoutStep{
		substeps:  substeps,
		timeout:   timeout,
		cancelled: make(chan struct{}),
	}
}

func (step *TimeoutStep) Perform() error {
	timedOut := make(chan struct{})
	timer := time.AfterFunc(step.timeout, func() {
		close(timedOut)
	})
	defer timer.Stop()

	for _, substep := range step.substeps {
		performDone := make(chan struct{})

		go func() {
			select {
			case <-timedOut:
				substep.Cancel()
			case <-step.cancelled:
				substep.Cancel()
			case <-performDone:
				return
			}
		}()

		performErr := substep.Perform()
		close(performDone)

		select {
		case <-timedOut:
			return TimeoutError{performErr}
		case <-step.cancelled:
			return CancelError{performErr}
		default:
		}

		if performErr != nil {
			return performErr
		}
	}

	return nil
}

func (step *TimeoutStep) Cancel() {
	close(step.cancelled)
}
