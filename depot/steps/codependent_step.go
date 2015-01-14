package steps

type codependentStep struct {
	substeps []Step
}

func NewCodependent(substeps []Step) *codependentStep {
	return &codependentStep{
		substeps: substeps,
	}
}

func (step *codependentStep) Perform() error {
	errs := make(chan error, len(step.substeps))

	for _, step := range step.substeps {
		go func(step Step) {
			errs <- step.Perform()
		}(step)
	}

	var firstFailure error

	for _ = range step.substeps {
		err := <-errs
		if err != nil && firstFailure == nil {
			firstFailure = err
			step.Cancel()
		}
	}

	return firstFailure
}

func (step *codependentStep) Cancel() {
	for _, substep := range step.substeps {
		substep.Cancel()
	}
}
