package steps

import "sync"

type codependentStep struct {
	substeps []Step

	cancel chan struct{}
	done   chan struct{}
}

func NewCodependent(substeps []Step) *codependentStep {
	return &codependentStep{
		substeps: substeps,

		cancel: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (step *codependentStep) Perform() error {
	defer close(step.done)

	errs := make(chan error, len(step.substeps))

	for _, step := range step.substeps {
		go func(step Step) {
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

func (step *codependentStep) Cancel() {
	close(step.cancel)
	<-step.done
}

func (step *codependentStep) actuallyCancel() {
	canceled := new(sync.WaitGroup)

	canceled.Add(len(step.substeps))
	for _, step := range step.substeps {
		go func(step Step) {
			defer canceled.Done()
			step.Cancel()
		}(step)
	}

	canceled.Wait()
}
