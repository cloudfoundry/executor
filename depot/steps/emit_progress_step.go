package steps

import (
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"

	"github.com/pivotal-golang/lager"
)

type EmitProgressStep struct {
	substep        Step
	logger         lager.Logger
	startMessage   string
	successMessage string
	failureMessage string
	streamer       log_streamer.LogStreamer
}

func NewEmitProgress(
	substep Step,
	startMessage,
	successMessage,
	failureMessage string,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
) *EmitProgressStep {
	logger = logger.Session("EmitProgressAction")
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
			emittableError, ok := err.(*EmittableError)
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
