package log_streamer

import "unicode/utf8"

type streamDestination struct {
	guid string

	emitter func(string, string)

	buffer []byte
}

func (destination *streamDestination) Write(data []byte) (int, error) {
	destination.processMessage(string(data))
	return len(data), nil
}

func (destination *streamDestination) flush() {
	if len(destination.buffer) > 0 {
		destination.emitter(destination.guid, string(destination.buffer))
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
