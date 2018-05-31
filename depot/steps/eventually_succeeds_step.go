package steps

import (
	"os"
	"time"

	"code.cloudfoundry.org/clock"
)

type eventuallySucceedsStep struct {
	create             func() Step
	frequency, timeout time.Duration
	clock              clock.Clock
}

// TODO: use a workpool when running the substep
func NewEventuallySucceedsStep(create func() Step, frequency, timeout time.Duration, clock clock.Clock) *eventuallySucceedsStep {
	return &eventuallySucceedsStep{
		create:    create,
		frequency: frequency,
		timeout:   timeout,
		clock:     clock,
	}
}

func (step *eventuallySucceedsStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	errCh := make(chan error, 1)
	var err error

	startTime := step.clock.Now()
	t := step.clock.NewTimer(step.frequency)

	for {
		select {
		case <-t.C():
		case <-signals:
			return ErrCancelled
		}

		eventualStep := step.create()
		go func() {
			errCh <- eventualStep.Perform()
		}()

		select {
		case err = <-errCh:
			if err == nil {
				return nil
			}
		case <-signals:
			eventualStep.Cancel()
			return <-errCh
		}

		if step.timeout > 0 && step.clock.Now().After(startTime.Add(step.timeout)) {
			return err
		}

		t.Reset(step.frequency)
	}
}
