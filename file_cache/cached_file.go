package file_cache

import (
	"os"
	"sync"
)

//Unlocks itself when closed
type CachedFile struct {
	file *os.File
	lock *sync.RWMutex
}

func NewCachedFile(file *os.File, lock *sync.RWMutex) *CachedFile {
	return &CachedFile{
		file: file,
		lock: lock,
	}
}

func (f *CachedFile) Read(p []byte) (int, error) {
	return f.file.Read(p)
}

func (f *CachedFile) Close() error {
	f.lock.RUnlock()
	return f.file.Close()
}
