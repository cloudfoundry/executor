package log_streamer

import (
	"bytes"
	"io"
	"sync"
)

type ConcurrentBuffer struct {
	Buffer     *bytes.Buffer
	Mutex      *sync.Mutex
	sourceName string
}

func NewConcurrentBuffer(payload *bytes.Buffer) *ConcurrentBuffer {
	if payload == nil {
		return nil
	}
	return &ConcurrentBuffer{
		Buffer:     payload,
		Mutex:      &sync.Mutex{},
		sourceName: DefaultLogSource,
	}
}

func (b *ConcurrentBuffer) Stdout() io.Writer {
	return b.Buffer
}

func (b *ConcurrentBuffer) Stderr() io.Writer {
	return b.Buffer
}
func (b *ConcurrentBuffer) Write(buf []byte) (int, error) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()
	return b.Buffer.Write(buf)
}

func (b *ConcurrentBuffer) Flush() {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()
}

func (b *ConcurrentBuffer) WithSource(sourceName string) LogStreamer {
	b.sourceName = sourceName
	return b
}

func (b *ConcurrentBuffer) SourceName() string {
	return b.sourceName
}

func (b *ConcurrentBuffer) Read(buf []byte) (int, error) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()
	return b.Buffer.Read(buf)
}

func (b *ConcurrentBuffer) Reset() {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()
	b.Buffer.Reset()
}

func (b *ConcurrentBuffer) Stop() {}
