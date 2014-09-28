package cacheddownloader

import (
	"io"
	"os"
	"runtime"
)

type fileCloser struct {
	file    *os.File
	onClose func(string)
}

func NewFileCloser(file *os.File, onClose func(string)) io.ReadCloser {
	fc := &fileCloser{
		file:    file,
		onClose: onClose,
	}

	runtime.SetFinalizer(fc, func(f *fileCloser) {
		f.Close()
	})

	return fc
}

func (fw *fileCloser) Read(p []byte) (int, error) {
	return fw.file.Read(p)
}

func (fw *fileCloser) Close() error {
	err := fw.file.Close()
	if err != nil {
		return err
	}
	fw.onClose(fw.file.Name())
	runtime.SetFinalizer(fw, nil)
	return nil
}
