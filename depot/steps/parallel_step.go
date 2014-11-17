package steps

type ParallelStep struct {
	substeps []Step
}

func NewParallel(substeps []Step) *ParallelStep {
	return &ParallelStep{
		substeps: substeps,
	}
}

func (step *ParallelStep) Perform() error {
	errs := make(chan error, len(step.substeps))

	for _, step := range step.substeps {
		go func(step Step) {
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
