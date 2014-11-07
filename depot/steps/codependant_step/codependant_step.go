package codependant_step

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor/depot/sequence"
)

type CodependantStep struct {
	substeps []sequence.Step

	cancel chan struct{}
	done   chan struct{}
}

func New(substeps []sequence.Step) *CodependantStep {
	return &CodependantStep{
		substeps: substeps,

		cancel: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (step *CodependantStep) Perform() error {
	defer close(step.done)

	errs := make(chan error, len(step.substeps))

	for _, step := range step.substeps {
		go func(step sequence.Step) {
			errs <- step.Perform()
		}(step)
	}

	for _ = range step.substeps {
		select {
		case stepErr := <-errs:
			if stepErr != nil {
				step.actuallyCancel()
				return stepErr
			}

		case <-step.cancel:
			step.actuallyCancel()
			return nil
		}
	}

	return nil
}

func (step *CodependantStep) Cancel() {
	close(step.cancel)
	<-step.done
}

func (step *CodependantStep) Cleanup() {
	for _, step := range step.substeps {
		step.Cleanup()
	}
}

func (step *CodependantStep) actuallyCancel() {
	canceled := new(sync.WaitGroup)

	canceled.Add(len(step.substeps))
	for _, step := range step.substeps {
		go func(step sequence.Step) {
			defer canceled.Done()
			step.Cancel()
		}(step)
	}

	canceled.Wait()
}
