package steps

import (
	"fmt"
	"time"
)

type timeoutStep struct {
	substep    Step
	timeout    time.Duration
	cancelChan chan struct{}
}

func NewTimeout(substep Step, timeout time.Duration) *timeoutStep {
	return &timeoutStep{
		substep:    substep,
		timeout:    timeout,
		cancelChan: make(chan struct{}),
	}
}

func (step *timeoutStep) Perform() error {
	resultChan := make(chan error, 1)
	timer := time.NewTimer(step.timeout)
	defer timer.Stop()

	go func() {
		resultChan <- step.substep.Perform()
	}()

	for {
		select {
		case err := <-resultChan:
			return err
		case <-timer.C:
			step.substep.Cancel()
			err := <-resultChan
			return TimeoutError{step.timeout, err}
		case <-step.cancelChan:
			step.substep.Cancel()
			err := <-resultChan
			return CancelError{err}
		}
	}
}

func (step *timeoutStep) Cancel() {
	close(step.cancelChan)
}

type TimeoutError struct {
	timeout      time.Duration
	SubstepError error
}

func (te TimeoutError) Error() string {
	return "Substep exceeded " + te.timeout.String() + " timeout; " + cancelledSubstepErrorMessage(te.SubstepError)
}

type CancelError struct {
	SubstepError error
}

func (ce CancelError) Error() string {
	return "Substep was cancelled; " + cancelledSubstepErrorMessage(ce.SubstepError)
}

func cancelledSubstepErrorMessage(err error) string {
	if err == nil {
		return "cancelled substep did not return error"
	}

	return fmt.Sprintf("cancelled substep error: %s", err)
}
