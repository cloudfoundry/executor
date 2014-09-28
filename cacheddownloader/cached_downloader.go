package cacheddownloader

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"sync"
	"time"
)

type CachedDownloader interface {
	Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error)
}

type CachingInfoType struct {
	ETag         string
	LastModified string
}

type cachedDownloader struct {
	downloader   *Downloader
	uncachedPath string
	cache        *FileCache

	lock       *sync.Mutex
	inProgress map[string]chan interface{}
}

func (c CachingInfoType) isCacheable() bool {
	return c.ETag != "" || c.LastModified != ""
}

func (c CachingInfoType) Equal(other CachingInfoType) bool {
	return c.ETag == other.ETag && c.LastModified == other.LastModified
}

func New(cachedPath string, uncachedPath string, maxSizeInBytes int64, downloadTimeout time.Duration) *cachedDownloader {
	os.RemoveAll(cachedPath)
	os.MkdirAll(cachedPath, 0770)
	return &cachedDownloader{
		downloader:   NewDownloader(downloadTimeout),
		uncachedPath: uncachedPath,
		cache:        NewCache(cachedPath, maxSizeInBytes),
		lock:         &sync.Mutex{},
		inProgress:   map[string]chan interface{}{},
	}
}

func (c *cachedDownloader) Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	if cacheKey == "" {
		return c.fetchUncachedFile(url)
	}

	cacheKey = fmt.Sprintf("%x", md5.Sum([]byte(cacheKey)))
	return c.fetchCachedFile(url, cacheKey)
}

func (c *cachedDownloader) fetchUncachedFile(url *url.URL) (io.ReadCloser, error) {
	download, err := c.downloadFile(url, "uncached", CachingInfoType{})
	if err != nil {
		return nil, err
	}

	return tempFileRemoveOnClose(download.path)
}

func (c *cachedDownloader) fetchCachedFile(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	rateLimiter := c.acquireLimiter(cacheKey)
	defer c.releaseLimiter(cacheKey, rateLimiter)

	currentReader, currentCachingInfo, getErr := c.cache.Get(cacheKey)

	download, err := c.downloadFile(url, cacheKey, currentCachingInfo)
	if err != nil {
		return nil, err
	}

	if download.path == "" {
		return currentReader, getErr
	}

	if currentReader != nil {
		currentReader.Close()
	}

	var newReader io.ReadCloser
	if download.cachingInfo.isCacheable() {
		newReader, err = c.cache.Add(cacheKey, download.path, download.size, download.cachingInfo)
		if err == NotEnoughSpace {
			return tempFileRemoveOnClose(download.path)
		}
	} else {
		c.cache.Remove(cacheKey)
		newReader, err = tempFileRemoveOnClose(download.path)
	}

	return newReader, err
}

func (c *cachedDownloader) acquireLimiter(cacheKey string) chan interface{} {
	for {
		c.lock.Lock()
		rateLimiter := c.inProgress[cacheKey]
		if rateLimiter == nil {
			rateLimiter = make(chan interface{})
			c.inProgress[cacheKey] = rateLimiter
			c.lock.Unlock()
			return rateLimiter
		}
		c.lock.Unlock()
		<-rateLimiter
	}
}

func (c *cachedDownloader) releaseLimiter(cacheKey string, limiter chan interface{}) {
	c.lock.Lock()
	delete(c.inProgress, cacheKey)
	close(limiter)
	c.lock.Unlock()
}

func tempFileRemoveOnClose(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return NewFileCloser(f, func(path string) {
		os.RemoveAll(path)
	}), nil
}

type download struct {
	path        string
	size        int64
	cachingInfo CachingInfoType
}

func (c *cachedDownloader) downloadFile(url *url.URL, name string, cachingInfo CachingInfoType) (download, error) {
	filename, size, cachingInfo, err := c.downloader.Download(url, func() (*os.File, error) {
		return ioutil.TempFile(c.uncachedPath, name+"-")
	}, cachingInfo)

	if err != nil {
		return download{}, err
	}

	return download{
		path:        filename,
		size:        size,
		cachingInfo: cachingInfo,
	}, nil
}
