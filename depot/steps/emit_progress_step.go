package steps

import (
	"os"

	"code.cloudfoundry.org/executor/depot/log_streamer"
	"github.com/tedsuo/ifrit"

	"code.cloudfoundry.org/lager"
)

type emitProgressStep struct {
	substep        ifrit.Runner
	logger         lager.Logger
	startMessage   string
	successMessage string
	failureMessage string
	streamer       log_streamer.LogStreamer
}

func NewEmitProgress(
	substep ifrit.Runner,
	startMessage,
	successMessage,
	failureMessage string,
	streamer log_streamer.LogStreamer,
	logger lager.Logger,
) *emitProgressStep {
	logger = logger.Session("emit-progress-step")
	return &emitProgressStep{
		substep:        substep,
		logger:         logger,
		startMessage:   startMessage,
		successMessage: successMessage,
		failureMessage: failureMessage,
		streamer:       streamer,
	}
}

func (step *emitProgressStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	if step.startMessage != "" {
		step.streamer.Stdout().Write([]byte(step.startMessage + "\n"))
	}

	err := step.substep.Run(signals, ready)

	if err != nil {
		if step.failureMessage != "" {
			step.streamer.Stderr().Write([]byte(step.failureMessage))

			if emittableError, ok := err.(*EmittableError); ok {
				step.streamer.Stderr().Write([]byte(": "))
				step.streamer.Stderr().Write([]byte(emittableError.Error()))

				wrappedMessage := ""
				if emittableError.WrappedError() != nil {
					wrappedMessage = emittableError.WrappedError().Error()
				}

				step.logger.Info("errored", lager.Data{
					"wrapped-error":   wrappedMessage,
					"message-emitted": emittableError.Error(),
				})
			}

			step.streamer.Stderr().Write([]byte("\n"))
		}
	} else {
		if step.successMessage != "" {
			step.streamer.Stdout().Write([]byte(step.successMessage + "\n"))
		}
	}

	return err
}
