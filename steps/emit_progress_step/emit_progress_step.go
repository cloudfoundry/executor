package emit_progress_step

import (
	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	steno "github.com/cloudfoundry/gosteno"
)

type EmitProgressStep struct {
	substep        sequence.Step
	logger         *steno.Logger
	startMessage   string
	successMessage string
	failureMessage string
	streamer       log_streamer.LogStreamer
}

func New(substep sequence.Step, startMessage, successMessage, failureMessage string, streamer log_streamer.LogStreamer, logger *steno.Logger) *EmitProgressStep {
	return &EmitProgressStep{
		substep:        substep,
		logger:         logger,
		startMessage:   startMessage,
		successMessage: successMessage,
		failureMessage: failureMessage,
		streamer:       streamer,
	}
}

func (step *EmitProgressStep) Perform() error {
	if step.startMessage != "" {
		step.streamer.Stdout().Write([]byte(step.startMessage + "\n"))
	}

	err := step.substep.Perform()
	if err != nil {
		if step.failureMessage != "" {
			step.streamer.Stderr().Write([]byte(step.failureMessage + "\n"))
			emittableError, ok := err.(*emittable_error.EmittableError)
			if ok {
				step.streamer.Stderr().Write([]byte(emittableError.EmittableError() + "\n"))
			}
		}
	} else {
		if step.successMessage != "" {
			step.streamer.Stdout().Write([]byte(step.successMessage + "\n"))
		}
	}

	return err
}

func (step *EmitProgressStep) Cancel() {
	step.substep.Cancel()
}

func (step *EmitProgressStep) Cleanup() {
	step.substep.Cleanup()
}
