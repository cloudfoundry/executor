package log_streamer

import "io"

type bufferStreamer struct {
	stdout io.Writer
	stderr io.Writer
}

func NewBufferStreamer(stdout, stderr io.Writer) LogStreamer {
	return &bufferStreamer{
		stdout: stdout,
		stderr: stderr,
	}
}

func (bs *bufferStreamer) Stdout() io.Writer {
	return bs.stdout
}

func (bs *bufferStreamer) Stderr() io.Writer {
	return bs.stderr
}

func (bs *bufferStreamer) Flush() {
}

func (bs *bufferStreamer) WithSource(sourceName string) LogStreamer {
	return bs
}
