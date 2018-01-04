package steps

import (
	"time"

	"code.cloudfoundry.org/clock"
)

type consistentlySucceedsStep struct {
	create             func() Step
	clock              clock.Clock
	frequency, timeout time.Duration
	*canceller
}

// TODO: use a workpool when running the substep
func NewConsistentlySucceedsStep(create func() Step, frequency time.Duration, clock clock.Clock) Step {
	return &consistentlySucceedsStep{
		create:    create,
		frequency: frequency,
		clock:     clock,
		canceller: newCanceller(),
	}
}

func (s *consistentlySucceedsStep) Perform() error {
	errCh := make(chan error, 1)
	t := s.clock.NewTimer(s.frequency)

	for {
		select {
		case <-s.Cancelled():
			return ErrCancelled
		case <-t.C():
		}

		step := s.create()

		go func() {
			errCh <- step.Perform()
		}()

		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		case <-s.Cancelled():
			step.Cancel()
			return <-errCh
		}

		t.Reset(s.frequency)
	}
}
