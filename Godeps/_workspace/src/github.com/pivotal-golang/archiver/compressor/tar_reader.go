package compressor

import (
	"io"
	"sync"
)

type tarReader struct {
	reader   io.Reader
	err      error
	errMutex *sync.RWMutex
}

func NewTarReader(srcPath string) io.Reader {
	reader, writer := io.Pipe()

	tarReader := &tarReader{
		reader:   reader,
		errMutex: new(sync.RWMutex),
	}

	go tarReader.writeTar(srcPath, writer)

	return tarReader
}

func (reader *tarReader) Read(buf []byte) (int, error) {
	n, err := reader.reader.Read(buf)
	if err != nil {
		if err := reader.getErr(); err != nil {
			return n, err
		}
	}

	return n, err
}

func (reader *tarReader) writeTar(srcPath string, writer io.WriteCloser) {
	err := WriteTar(srcPath, writer)
	reader.setErr(err)
	writer.Close()
}

func (reader *tarReader) setErr(err error) {
	reader.errMutex.Lock()
	reader.err = err
	reader.errMutex.Unlock()
}

func (reader *tarReader) getErr() error {
	reader.errMutex.RLock()
	defer reader.errMutex.RUnlock()
	return reader.err
}
