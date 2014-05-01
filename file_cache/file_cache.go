package file_cache

import (
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/cloudfoundry-incubator/executor/downloader"
)

type FileCache interface {
	Fetch(url string, cacheKey string) (io.ReadCloser, error)
}

type Cache struct {
	basePath       string
	maxSizeInBytes int
	downloader     downloader.Downloader
	fileLocks      map[string]*sync.RWMutex
	lock           *sync.Mutex
}

func New(basePath string, maxSizeInBytes int, downloader downloader.Downloader) *Cache {
	return &Cache{
		basePath:       basePath,
		maxSizeInBytes: maxSizeInBytes,
		downloader:     downloader,
		fileLocks:      make(map[string]*sync.RWMutex),
		lock:           &sync.Mutex{},
	}
}

func (c *Cache) Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	if cacheKey == "" {
		return c.fetchUncachedFile(url)
	} else {
		return c.fetchCachedFile(url, cacheKey)
	}
}

func (c *Cache) fetchUncachedFile(url *url.URL) (io.ReadCloser, error) {
	destinationFile, err := ioutil.TempFile(c.basePath, "uncached")
	if err != nil {
		return nil, err
	}

	_, err = c.downloader.Download(url, destinationFile)
	if err != nil {
		os.Remove(destinationFile.Name())
		return nil, err
	}
	destinationFile.Seek(0, 0)

	return NewSingleUseFile(destinationFile), nil
}

func (c *Cache) fetchCachedFile(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	lock := c.lockForCacheKey(cacheKey)

	//do we have the file?  is it up-to-date?
	lock.RLock()
	if c.hasUpToDateCachedFileForCacheKey(url, cacheKey) {
		destinationFile, err := os.Open(c.pathForCacheKey(cacheKey))
		if err != nil {
			lock.RUnlock()
			return nil, err
		}

		return NewCachedFile(destinationFile, lock), nil
	}
	lock.RUnlock()

	//download the file
	lock.Lock()
	destinationFile, err := c.downloadToCache(url, cacheKey)
	lock.Unlock()

	if err != nil {
		return nil, err
	}

	lock.RLock()
	return NewCachedFile(destinationFile, lock), err
}

func (c *Cache) pathForCacheKey(cacheKey string) string {
	return filepath.Join(c.basePath, cacheKey)
}

func (c *Cache) hasUpToDateCachedFileForCacheKey(url *url.URL, cacheKey string) bool {
	fileInfo, err := os.Stat(c.pathForCacheKey(cacheKey))
	if err != nil {
		return false
	}

	modified, err := c.downloader.ModifiedSince(url, fileInfo.ModTime())
	if err != nil {
		return false
	}

	return !modified
}

func (c *Cache) downloadToCache(url *url.URL, cacheKey string) (*os.File, error) {
	destinationFile, err := os.Create(c.pathForCacheKey(cacheKey))
	if err != nil {
		return nil, err
	}

	_, err = c.downloader.Download(url, destinationFile)
	if err != nil {
		os.Remove(destinationFile.Name())
		return nil, err
	}
	destinationFile.Seek(0, 0)

	return destinationFile, nil
}

func (c *Cache) lockForCacheKey(cacheKey string) *sync.RWMutex {
	c.lock.Lock()
	defer c.lock.Unlock()
	lock, ok := c.fileLocks[c.pathForCacheKey(cacheKey)]
	if !ok {
		lock = &sync.RWMutex{}
		c.fileLocks[c.pathForCacheKey(cacheKey)] = lock
	}

	return lock
}
