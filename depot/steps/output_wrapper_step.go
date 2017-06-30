package steps

import (
	"io"
	"io/ioutil"
)

type outputWrapperStep struct {
	substep Step
	reader  io.Reader
}

// This step ignores the error from the substep and returns the content of
// Reader as an emittable error. This is used to wrap the output of the
// healthcheck as the error instead of using the exit status or the process
func NewOutputWrapper(substep Step, reader io.Reader) *outputWrapperStep {
	return &outputWrapperStep{
		substep: substep,
		reader:  reader,
	}
}

func (step *outputWrapperStep) Perform() error {
	err := step.substep.Perform()
	if err != nil {
		bytes, err := ioutil.ReadAll(step.reader)
		if err != nil {
			return err
		}

		return NewEmittableError(nil, string(bytes))
	}

	return nil
}

func (step *outputWrapperStep) Cancel() {
	step.substep.Cancel()
}
