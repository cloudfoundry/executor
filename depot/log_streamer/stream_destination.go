package log_streamer

import (
	"sync"
	"unicode/utf8"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/go-loggregator/rpc/loggregator_v2"
)

type streamDestination struct {
	guid         string
	sourceName   string
	sourceId     string
	messageType  loggregator_v2.Log_Type
	buffer       []byte
	processLock  sync.Mutex
	metronClient loggingclient.IngressClient
}

func newStreamDestination(guid, sourceName, sourceId string, messageType loggregator_v2.Log_Type, metronClient loggingclient.IngressClient) *streamDestination {
	return &streamDestination{
		guid:         guid,
		sourceName:   sourceName,
		sourceId:     sourceId,
		messageType:  messageType,
		buffer:       make([]byte, 0, MAX_MESSAGE_SIZE),
		metronClient: metronClient,
	}
}

func (destination *streamDestination) lockAndFlush() {
	destination.processLock.Lock()
	defer destination.processLock.Unlock()
	destination.flush()
}

func (destination *streamDestination) Write(data []byte) (int, error) {
	destination.processMessage(string(data))
	return len(data), nil
}

func (destination *streamDestination) flush() {
	msg := destination.copyAndResetBuffer()

	if len(msg) > 0 {
		switch destination.messageType {
		case loggregator_v2.Log_OUT:
			destination.metronClient.SendAppLog(destination.guid, string(msg), destination.sourceName, destination.sourceId)
		case loggregator_v2.Log_ERR:
			destination.metronClient.SendAppErrorLog(destination.guid, string(msg), destination.sourceName, destination.sourceId)
		}
	}
}

// Not thread safe.  should only be called when holding the processLock
func (destination *streamDestination) copyAndResetBuffer() []byte {
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
	destination.processLock.Lock()
	defer destination.processLock.Unlock()

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

// Not thread safe.  should only be called when holding the processLock
func (destination *streamDestination) appendToBuffer(message string) string {
	if len(message)+len(destination.buffer) >= MAX_MESSAGE_SIZE {
		remainingSpaceInBuffer := MAX_MESSAGE_SIZE - len(destination.buffer)
		destination.buffer = append(destination.buffer, []byte(message[0:remainingSpaceInBuffer])...)

		r, _ := utf8.DecodeLastRune(destination.buffer[0:len(destination.buffer)])

		// if we error initially, go back to preserve utf8 boundaries
		bytesToCut := 0
		for r == utf8.RuneError && bytesToCut < 3 {
			bytesToCut++
			r, _ = utf8.DecodeLastRune(destination.buffer[0 : len(destination.buffer)-bytesToCut])
		}

		index := remainingSpaceInBuffer - bytesToCut
		if index < 0 {
			index = 0
			destination.buffer = destination.buffer[0 : len(destination.buffer)-remainingSpaceInBuffer]
		} else {
			destination.buffer = destination.buffer[0 : len(destination.buffer)-bytesToCut]
		}

		return message[index:]
	}

	destination.buffer = append(destination.buffer, []byte(message)...)
	return ""
}

func (d *streamDestination) withSource(sourceName string) *streamDestination {
	return newStreamDestination(d.guid, sourceName, d.sourceId, d.messageType, d.metronClient)
}
