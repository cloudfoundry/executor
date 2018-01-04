package steps

import (
	"io"
	"io/ioutil"
	"strings"
)

type outputWrapperStep struct {
	substep Step
	prefix  string
	reader  io.Reader
}

// This step ignores the error from the substep and returns the content of
// Reader as an emittable error. This is used to wrap the output of the
// healthcheck as the error instead of using the exit status or the process
func NewOutputWrapper(substep Step, reader io.Reader) *outputWrapperStep {
	return NewOutputWrapperWithPrefix(substep, reader, "")
}

func NewOutputWrapperWithPrefix(substep Step, reader io.Reader, prefix string) *outputWrapperStep {
	return &outputWrapperStep{
		substep: substep,
		reader:  reader,
		prefix:  prefix,
	}
}

func (step *outputWrapperStep) Perform() error {
	substepErr := step.substep.Perform()
	if substepErr != nil {
		bytes, err := ioutil.ReadAll(step.reader)
		if err != nil {
			return err
		}

		readerErr := string(bytes)
		if readerErr != "" {
			msg := strings.TrimSpace(readerErr)
			if step.prefix != "" {
				msg = step.prefix + ": " + msg
			}
			return NewEmittableError(nil, msg)
		}

		return substepErr
	}

	return nil
}

func (step *outputWrapperStep) Cancel() {
	step.substep.Cancel()
}
