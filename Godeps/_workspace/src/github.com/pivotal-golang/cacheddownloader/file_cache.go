package cacheddownloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileCache struct {
	cachedPath     string
	maxSizeInBytes int64
	lock           *sync.Mutex
	entries        map[string]fileCacheEntry
	cacheFilePaths map[string]string
	seq            uint64
}

type fileCacheEntry struct {
	size        int64
	access      time.Time
	cachingInfo CachingInfoType
	filePath    string
}

func NewCache(dir string, maxSizeInBytes int64) *FileCache {
	return &FileCache{
		cachedPath:     dir,
		maxSizeInBytes: maxSizeInBytes,
		lock:           &sync.Mutex{},
		entries:        map[string]fileCacheEntry{},
		cacheFilePaths: map[string]string{},
		seq:            0,
	}
}

func (c *FileCache) Add(cacheKey string, sourcePath string, size int64, cachingInfo CachingInfoType) (bool, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.unsafelyRemoveCacheEntryFor(cacheKey)

	if size > c.maxSizeInBytes {
		//file does not fit in cache...
		return false, nil
	}

	c.makeRoom(size)

	c.seq++
	uniqueName := fmt.Sprintf("%s-%d-%d", cacheKey, time.Now().UnixNano(), c.seq)
	cachePath := filepath.Join(c.cachedPath, uniqueName)

	err := os.Rename(sourcePath, cachePath)
	if err != nil {
		return false, err
	}

	c.cacheFilePaths[cachePath] = cacheKey
	c.entries[cacheKey] = fileCacheEntry{
		size:        size,
		filePath:    cachePath,
		access:      time.Now(),
		cachingInfo: cachingInfo,
	}

	return true, nil
}

func (c *FileCache) Get(cacheKey string) (io.ReadCloser, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	path := c.entries[cacheKey].filePath
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	readCloser := NewFileCloser(f, func(filePath string) {
		c.removeFileIfUntracked(filePath)
	})

	return readCloser, nil
}

func (c *FileCache) RemoveEntry(cacheKey string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.unsafelyRemoveCacheEntryFor(cacheKey)
}

func (c *FileCache) RecordAccess(cacheKey string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	f := c.entries[cacheKey]
	f.access = time.Now()
	c.entries[cacheKey] = f
}

func (c *FileCache) removeFileIfUntracked(cacheFilePath string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	_, isTracked := c.cacheFilePaths[cacheFilePath]
	if !isTracked {
		os.RemoveAll(cacheFilePath)
	}
}

func (c *FileCache) Info(cacheKey string) CachingInfoType {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.entries[cacheKey].cachingInfo
}

func (c *FileCache) makeRoom(size int64) {
	usedSpace := c.usedSpace()
	for c.maxSizeInBytes < usedSpace+size {
		oldestAccessTime, oldestCacheKey := time.Now(), ""
		for ck, f := range c.entries {
			if f.access.Before(oldestAccessTime) {
				oldestCacheKey = ck
				oldestAccessTime = f.access
			}
		}

		usedSpace -= c.entries[oldestCacheKey].size
		c.unsafelyRemoveCacheEntryFor(oldestCacheKey)
	}
}

func (c *FileCache) unsafelyRemoveCacheEntryFor(cacheKey string) {
	fp := c.entries[cacheKey].filePath

	if fp != "" {
		delete(c.cacheFilePaths, fp)
		os.RemoveAll(fp)
	}
	delete(c.entries, cacheKey)
}

func (c *FileCache) usedSpace() int64 {
	space := int64(0)
	for _, f := range c.entries {
		space += f.size
	}
	return space
}
