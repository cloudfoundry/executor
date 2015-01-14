package steps

import "github.com/hashicorp/go-multierror"

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

	var aggregate *multierror.Error
	var cancelled bool

	for _ = range step.substeps {
		err := <-errs
		if err != nil {
			aggregate = multierror.Append(aggregate, err)

			if !cancelled {
				cancelled = true
				step.Cancel()
			}
		}
	}

	return aggregate.ErrorOrNil()
}

func (step *codependentStep) Cancel() {
	for _, substep := range step.substeps {
		substep.Cancel()
	}
}
