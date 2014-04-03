package log_streamer

import (
	"io"

	"github.com/cloudfoundry/loggregatorlib/emitter"
)

const MAX_MESSAGE_SIZE = 4096

type LogStreamer interface {
	Stdout() io.Writer
	Stderr() io.Writer

	Flush()
}

type logStreamer struct {
	stdout *streamDestination
	stderr *streamDestination
}

func New(guid string, loggregatorEmitter emitter.Emitter) *logStreamer {
	return &logStreamer{
		stdout: &streamDestination{
			guid:    guid,
			emitter: loggregatorEmitter.Emit,
			buffer:  make([]byte, 0, MAX_MESSAGE_SIZE),
		},

		stderr: &streamDestination{
			guid:    guid,
			emitter: loggregatorEmitter.EmitError,
			buffer:  make([]byte, 0, MAX_MESSAGE_SIZE),
		},
	}
}

func (e *logStreamer) Stdout() io.Writer {
	return e.stdout
}

func (e *logStreamer) Stderr() io.Writer {
	return e.stderr
}

func (e *logStreamer) Flush() {
	e.stdout.flush()
	e.stderr.flush()
}
