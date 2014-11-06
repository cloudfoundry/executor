package log_streamer

import (
	"sync"
	"time"
	"unicode/utf8"

	"code.google.com/p/goprotobuf/proto"

	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/cloudfoundry/dropsonde/events"
)

type streamDestination struct {
	guid        string
	sourceName  string
	sourceId    string
	messageType events.LogMessage_MessageType
	emitter     logemitter.Emitter
	buffer      []byte
	bufferLock  sync.Mutex
}

func newStreamDestination(guid, sourceName, sourceId string, messageType events.LogMessage_MessageType, em logemitter.Emitter) *streamDestination {
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
	msg := destination.copyAndResetBuffer()

	if len(msg) > 0 {
		destination.emitter.EmitLogMessage(&events.LogMessage{
			AppId:          &destination.guid,
			SourceType:     &destination.sourceName,
			SourceInstance: &destination.sourceId,
			Message:        msg,
			MessageType:    &destination.messageType,
			Timestamp:      proto.Int64(time.Now().UnixNano()),
		})
	}
}

func (destination *streamDestination) copyAndResetBuffer() []byte {
	destination.bufferLock.Lock()
	defer destination.bufferLock.Unlock()

	if len(destination.buffer) > 0 {
		msg := make([]byte, len(destination.buffer))
		copy(msg, destination.buffer)

		destination.buffer = destination.buffer[:0]
		return msg
	}

	return []byte{}
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
	for {
		message = destination.appendToBuffer(message)
		if len(message) == 0 {
			break
		}
		destination.flush()
	}

	if terminates {
		destination.flush()
	}
}

func (destination *streamDestination) appendToBuffer(message string) string {
	destination.bufferLock.Lock()
	defer destination.bufferLock.Unlock()

	if len(message)+len(destination.buffer) >= MAX_MESSAGE_SIZE {
		remainingSpaceInBuffer := MAX_MESSAGE_SIZE - len(destination.buffer)
		destination.buffer = append(destination.buffer, []byte(message[0:remainingSpaceInBuffer])...)

		r, _ := utf8.DecodeLastRune(destination.buffer)
		if r == utf8.RuneError {
			destination.buffer = destination.buffer[0 : len(destination.buffer)-1]
			message = message[remainingSpaceInBuffer-1:]
		} else {
			message = message[remainingSpaceInBuffer:]
		}

		return message
	}

	destination.buffer = append(destination.buffer, []byte(message)...)
	return ""
}
