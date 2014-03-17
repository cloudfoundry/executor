package log_streamer

import (
	"unicode/utf8"

	"github.com/cloudfoundry/loggregatorlib/emitter"
)

const MAX_MESSAGE_SIZE = 4096

type LogStreamer interface {
	StreamStdout(message string)
	StreamStderr(message string)
	Flush()
}

type streamSource int

const (
	streamSourceInvalid streamSource = iota
	streamSourceStdout
	streamSourceStderr
)

type logStreamer struct {
	guid string

	loggregatorEmitter emitter.Emitter

	buffers map[streamSource][]byte
}

func New(guid string, loggregatorEmitter emitter.Emitter) *logStreamer {
	return &logStreamer{
		guid:               guid,
		loggregatorEmitter: loggregatorEmitter,
		buffers: map[streamSource][]byte{
			streamSourceStdout: make([]byte, 0, MAX_MESSAGE_SIZE),
			streamSourceStderr: make([]byte, 0, MAX_MESSAGE_SIZE),
		},
	}
}

func (e *logStreamer) StreamStdout(message string) {
	e.processMessage(message, streamSourceStdout)
}

func (e *logStreamer) StreamStderr(message string) {
	e.processMessage(message, streamSourceStderr)
}

func (e *logStreamer) Flush() {
	e.flushSource(streamSourceStdout)
	e.flushSource(streamSourceStderr)
}

func (e *logStreamer) flushSource(source streamSource) {
	if len(e.buffers[source]) > 0 {
		if source == streamSourceStdout {
			e.loggregatorEmitter.Emit(e.guid, string(e.buffers[source]))

		} else {
			e.loggregatorEmitter.EmitError(e.guid, string(e.buffers[source]))
		}

		e.buffers[source] = e.buffers[source][:0]
	}
}

func (e *logStreamer) processMessage(message string, source streamSource) {
	start := 0
	for i, rune := range message {
		if rune == '\n' || rune == '\r' {
			e.processString(message[start:i], source, true)
			start = i + 1
		}
	}
	if start < len(message) {
		e.processString(message[start:], source, false)
	}
}

func (e *logStreamer) processString(message string, source streamSource, terminates bool) {
	for len(message)+len(e.buffers[source]) >= MAX_MESSAGE_SIZE {
		remainingSpaceInBuffer := MAX_MESSAGE_SIZE - len(e.buffers[source])
		e.buffers[source] = append(e.buffers[source], []byte(message[0:remainingSpaceInBuffer])...)

		r, _ := utf8.DecodeLastRune(e.buffers[source])
		if r == utf8.RuneError {
			e.buffers[source] = e.buffers[source][0 : len(e.buffers[source])-1]
			message = message[remainingSpaceInBuffer-1:]
		} else {
			message = message[remainingSpaceInBuffer:]
		}

		e.flushSource(source)
	}

	e.buffers[source] = append(e.buffers[source], []byte(message)...)

	if terminates {
		e.flushSource(source)
	}
}
