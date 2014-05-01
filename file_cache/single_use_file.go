package file_cache

import "os"

type SingleUseFile struct {
	file *os.File
}

//Deletes itself when closed
func NewSingleUseFile(file *os.File) *SingleUseFile {
	return &SingleUseFile{
		file: file,
	}
}

func (f *SingleUseFile) Read(p []byte) (n int, err error) {
	return f.file.Read(p)
}

func (f *SingleUseFile) Close() error {
	err := f.file.Close()
	if err != nil {
		return err
	}

	return os.Remove(f.file.Name())
}
