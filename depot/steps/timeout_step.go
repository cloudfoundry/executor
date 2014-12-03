package steps

import (
	"fmt"
	"time"

	"github.com/pivotal-golang/lager"
)

type timeoutStep struct {
	substep    Step
	timeout    time.Duration
	cancelChan chan struct{}
	logger     lager.Logger
}

func NewTimeout(substep Step, timeout time.Duration, logger lager.Logger) *timeoutStep {
	return &timeoutStep{
		substep:    substep,
		timeout:    timeout,
		cancelChan: make(chan struct{}),
		logger:     logger.Session("TimeoutAction"),
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
			timeoutErr := TimeoutError{
				timeout:      step.timeout,
				SubstepError: err,
			}
			step.logger.Error("timed-out", timeoutErr)
			return timeoutErr
		case <-step.cancelChan:
			step.substep.Cancel()
			err := <-resultChan
			return CancelError{err}
		}
	}
}

func (step *timeoutStep) Cancel() {
	step.logger.Info("cancelling")
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
