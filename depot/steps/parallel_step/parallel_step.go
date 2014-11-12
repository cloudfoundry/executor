package parallel_step

import "github.com/cloudfoundry-incubator/executor/depot/sequence"

type ParallelStep struct {
	substeps []sequence.Step
}

func New(substeps []sequence.Step) *ParallelStep {
	return &ParallelStep{
		substeps: substeps,
	}
}

func (step *ParallelStep) Perform() error {
	errs := make(chan error, len(step.substeps))

	for _, step := range step.substeps {
		go func(step sequence.Step) {
			errs <- step.Perform()
		}(step)
	}

	var err error
	for _ = range step.substeps {
		stepErr := <-errs
		if stepErr != nil {
			err = stepErr
		}
	}

	return err
}

func (step *ParallelStep) Cancel() {
	for _, step := range step.substeps {
		step.Cancel()
	}
}
