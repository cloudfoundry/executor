package log_streamer

import (
	"io"
	"sync"
)

type bufferStreamer struct {
	stdout io.Writer
	stderr io.Writer
}

func NewBufferStreamer(stdout, stderr io.Writer) LogStreamer {
	return &bufferStreamer{
		stdout: newConcurrentWriter(stdout),
		stderr: newConcurrentWriter(stderr),
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

type concurrentWriter struct {
	inner io.Writer
	lock  sync.Mutex
}

func newConcurrentWriter(w io.Writer) io.Writer {
	return &concurrentWriter{
		inner: w,
		lock:  sync.Mutex{},
	}
}

func (w *concurrentWriter) Write(data []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.inner.Write(data)
}
