package log_streamer

import (
	"io"
	"strconv"

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

func New(guid string, sourceName string, index *int, loggregatorEmitter emitter.Emitter) LogStreamer {
	if guid == "" {
		return noopStreamer{}
	}

	sourceIndex := "0"
	if index != nil {
		sourceIndex = strconv.Itoa(*index)
	}

	return &logStreamer{
		stdout: newStreamDestination(
			guid,
			sourceName,
			sourceIndex,
			logmessage.LogMessage_OUT,
			loggregatorEmitter,
		),

		stderr: newStreamDestination(
			guid,
			sourceName,
			sourceIndex,
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
