package fakelogstreamer

type FakeLogStreamer struct {
	StreamedStdout []string
	StreamedStderr []string
	Flushed        bool
}

func New() *FakeLogStreamer {
	return &FakeLogStreamer{}
}

func (e *FakeLogStreamer) StreamStdout(message string) {
	e.StreamedStdout = append(e.StreamedStdout, message)
}

func (e *FakeLogStreamer) StreamStderr(message string) {
	e.StreamedStderr = append(e.StreamedStderr, message)
}

func (e *FakeLogStreamer) Flush() {
	e.Flushed = true
}
