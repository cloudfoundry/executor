package log_streamer

import (
	"time"
	"unicode/utf8"

	"code.google.com/p/goprotobuf/proto"

	"github.com/cloudfoundry/loggregatorlib/emitter"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
)

type streamDestination struct {
	guid        string
	sourceName  string
	sourceId    string
	messageType logmessage.LogMessage_MessageType
	emitter     emitter.Emitter
	buffer      []byte
}

func newStreamDestination(guid, sourceName, sourceId string, messageType logmessage.LogMessage_MessageType, em emitter.Emitter) *streamDestination {
	return &streamDestination{
		guid:        guid,
		sourceName:  sourceName,
		sourceId:    sourceId,
		messageType: messageType,
		emitter:     em,
		buffer:      make([]byte, 0, MAX_MESSAGE_SIZE),
	}
}

func (destination *streamDestination) Write(data []byte) (int, error) {
	destination.processMessage(string(data))
	return len(data), nil
}

func (destination *streamDestination) flush() {
	if len(destination.buffer) > 0 {
		msg := make([]byte, len(destination.buffer))
		copy(msg, destination.buffer)

		destination.emitter.EmitLogMessage(&logmessage.LogMessage{
			AppId:       &destination.guid,
			SourceName:  &destination.sourceName,
			SourceId:    &destination.sourceId,
			Message:     msg,
			MessageType: &destination.messageType,
			Timestamp:   proto.Int64(time.Now().UnixNano()),
		})
		destination.buffer = destination.buffer[:0]
	}
}

func (destination *streamDestination) processMessage(message string) {
	start := 0
	for i, rune := range message {
		if rune == '\n' || rune == '\r' {
			destination.processString(message[start:i], true)
			start = i + 1
		}
	}

	if start < len(message) {
		destination.processString(message[start:], false)
	}
}

func (destination *streamDestination) processString(message string, terminates bool) {
	for len(message)+len(destination.buffer) >= MAX_MESSAGE_SIZE {
		remainingSpaceInBuffer := MAX_MESSAGE_SIZE - len(destination.buffer)
		destination.buffer = append(destination.buffer, []byte(message[0:remainingSpaceInBuffer])...)

		r, _ := utf8.DecodeLastRune(destination.buffer)
		if r == utf8.RuneError {
			destination.buffer = destination.buffer[0 : len(destination.buffer)-1]
			message = message[remainingSpaceInBuffer-1:]
		} else {
			message = message[remainingSpaceInBuffer:]
		}

		destination.flush()
	}

	destination.buffer = append(destination.buffer, []byte(message)...)

	if terminates {
		destination.flush()
	}
}
