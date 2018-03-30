package steps

import (
	"time"

	"code.cloudfoundry.org/lager"

	"code.cloudfoundry.org/clock"
)

type eventuallySucceedsStep struct {
	create             func() Step
	frequency, timeout time.Duration
	clock              clock.Clock
	logger             lager.Logger
	*canceller
}

// TODO: use a workpool when running the substep
func NewEventuallySucceedsStep(create func() Step, logger lager.Logger, frequency, timeout time.Duration, clock clock.Clock) Step {
	return &eventuallySucceedsStep{
		create:    create,
		frequency: frequency,
		timeout:   timeout,
		clock:     clock,
		canceller: newCanceller(),
		logger:    logger,
	}
}

func (s *eventuallySucceedsStep) Perform() error {
	errCh := make(chan error, 1)
	var err error

	startTime := s.clock.Now()
	t := s.clock.NewTimer(s.frequency)

	for {
		select {
		case <-t.C():
		case <-s.Cancelled():
			return ErrCancelled
		}

		step := s.create()
		go func() {
			err := step.Perform()
			errCh <- err
		}()

		select {
		case err = <-errCh:
			if err == nil {
				return nil
			}
		case <-s.Cancelled():
			step.Cancel()
			return <-errCh
		}

		if s.timeout > 0 && s.clock.Now().After(startTime.Add(s.timeout)) {
			return err
		}

		t.Reset(s.frequency)
	}
}
