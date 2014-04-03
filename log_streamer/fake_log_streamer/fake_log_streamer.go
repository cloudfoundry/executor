package fake_log_streamer

import (
	"bytes"
	"io"
)

type FakeLogStreamer struct {
	StdoutBuffer *bytes.Buffer
	StderrBuffer *bytes.Buffer

	Flushed bool
}

func New() *FakeLogStreamer {
	return &FakeLogStreamer{
		StdoutBuffer: new(bytes.Buffer),
		StderrBuffer: new(bytes.Buffer),
	}
}

func (e *FakeLogStreamer) Stdout() io.Writer {
	return e.StdoutBuffer
}

func (e *FakeLogStreamer) Stderr() io.Writer {
	return e.StderrBuffer
}

func (e *FakeLogStreamer) Flush() {
	e.Flushed = true
}
