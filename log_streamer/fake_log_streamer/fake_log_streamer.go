package fake_log_streamer

type FakeLogStreamer struct {
	StreamedStdout string
	StreamedStderr string
	Flushed        bool
}

func New() *FakeLogStreamer {
	return &FakeLogStreamer{}
}

func (e *FakeLogStreamer) StreamStdout(message string) {
	e.StreamedStdout += message
}

func (e *FakeLogStreamer) StreamStderr(message string) {
	e.StreamedStderr += message
}

func (e *FakeLogStreamer) Flush() {
	e.Flushed = true
}
