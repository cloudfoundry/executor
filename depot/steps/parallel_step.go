package steps

type parallelStep struct {
	substeps []Step
}

func NewParallel(substeps []Step) *parallelStep {
	return &parallelStep{
		substeps: substeps,
	}
}

func (step *parallelStep) Perform() error {
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

func (step *parallelStep) Cancel() {
	for _, step := range step.substeps {
		step.Cancel()
	}
}
