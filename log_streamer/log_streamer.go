package log_streamer

import (
	"io"

	"github.com/cloudfoundry/loggregatorlib/emitter"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
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

func New(guid string, sourceName string, loggregatorEmitter emitter.Emitter) LogStreamer {
	if guid == "" {
		return noopStreamer{}
	}

	return &logStreamer{
		stdout: newStreamDestination(
			guid,
			sourceName,
			"1",
			logmessage.LogMessage_OUT,
			loggregatorEmitter,
		),

		stderr: newStreamDestination(
			guid,
			sourceName,
			"1",
			logmessage.LogMessage_ERR,
			loggregatorEmitter,
		),
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
