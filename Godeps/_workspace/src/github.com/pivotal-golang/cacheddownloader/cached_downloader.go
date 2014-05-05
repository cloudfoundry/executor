package cacheddownloader

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
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

type CachedFile struct {
	size        int64
	access      time.Time
	cachingInfo CachingInfoType
}

type cachedDownloader struct {
	cachedPath     string
	uncachedPath   string
	maxSizeInBytes int64
	downloader     *Downloader
	lock           *sync.Mutex

	cachedFiles map[string]CachedFile
}

func New(cachedPath string, uncachedPath string, maxSizeInBytes int64, downloadTimeout time.Duration) *cachedDownloader {
	os.RemoveAll(cachedPath)
	os.MkdirAll(cachedPath, 0770)
	return &cachedDownloader{
		cachedPath:     cachedPath,
		uncachedPath:   uncachedPath,
		maxSizeInBytes: maxSizeInBytes,
		downloader:     NewDownloader(downloadTimeout),
		lock:           &sync.Mutex{},
		cachedFiles:    map[string]CachedFile{},
	}
}

func (c *cachedDownloader) Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	if cacheKey == "" {
		return c.fetchUncachedFile(url)
	} else {
		cacheKey = fmt.Sprintf("%x", md5.Sum([]byte(cacheKey)))
		return c.fetchCachedFile(url, cacheKey)
	}
}

func (c *cachedDownloader) fetchUncachedFile(url *url.URL) (io.ReadCloser, error) {
	destinationFile, err := ioutil.TempFile(c.uncachedPath, "uncached")
	if err != nil {
		return nil, err
	}

	_, _, _, err = c.downloader.Download(url, destinationFile, CachingInfoType{})
	if err != nil {
		os.Remove(destinationFile.Name())
		return nil, err
	}
	os.Remove(destinationFile.Name()) //OK, 'cause that's how unix works
	destinationFile.Seek(0, 0)

	return destinationFile, nil
}

func (c *cachedDownloader) fetchCachedFile(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	c.recordAccessForCacheKey(cacheKey)

	path := c.pathForCacheKey(cacheKey)

	f, err := os.Open(path)
	fileExists := err == nil

	//download the file to a temporary location
	tempFile, err := ioutil.TempFile(c.uncachedPath, "temporary")
	if err != nil {
		if fileExists {
			f.Close()
		}
		return nil, err
	}
	defer os.Remove(tempFile.Name()) //OK, even if we return tempFile 'cause that's how UNIX works.

	didDownload, size, cachingInfo, err := c.downloader.Download(url, tempFile, c.cachingInfoForCacheKey(cacheKey))
	if err != nil {
		if fileExists {
			f.Close()
		}
		return nil, err
	}

	if !didDownload {
		return f, nil
	}

	if cachingInfo.ETag == "" && cachingInfo.LastModified == "" {
		c.removeCacheEntryFor(cacheKey)
		_, err = tempFile.Seek(0, 0)
		if err != nil {
			return nil, err
		}

		return tempFile, nil
	}

	c.setCachingInfoForCacheKey(cacheKey, cachingInfo)

	//make room for the file and move it in (if possible)
	c.moveFileIntoCache(cacheKey, tempFile.Name(), size)

	_, err = tempFile.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	return tempFile, nil
}

func (c *cachedDownloader) pathForCacheKey(cacheKey string) string {
	return filepath.Join(c.cachedPath, cacheKey)
}

func (c *cachedDownloader) moveFileIntoCache(cacheKey string, sourcePath string, size int64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if size > c.maxSizeInBytes {
		//file does not fit in cache...
		return
	}

	usedSpace := int64(0)
	for ck, f := range c.cachedFiles {
		if ck != cacheKey {
			usedSpace += f.size
		}
	}

	for c.maxSizeInBytes < usedSpace+size {
		oldestAccessTime, oldestCacheKey := time.Now(), ""
		for ck, f := range c.cachedFiles {
			if ck != cacheKey {
				if f.access.Before(oldestAccessTime) {
					oldestCacheKey = ck
					oldestAccessTime = f.access
				}
			}
		}
		os.Remove(c.pathForCacheKey(oldestCacheKey))
		usedSpace -= c.cachedFiles[oldestCacheKey].size
		delete(c.cachedFiles, oldestCacheKey)
	}

	f := c.cachedFiles[cacheKey]
	f.size = size
	c.cachedFiles[cacheKey] = f
	os.Rename(sourcePath, c.pathForCacheKey(cacheKey))
}

func (c *cachedDownloader) removeCacheEntryFor(cacheKey string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cachedFiles, cacheKey)
	os.RemoveAll(c.pathForCacheKey(cacheKey))
}

func (c *cachedDownloader) recordAccessForCacheKey(cacheKey string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	f := c.cachedFiles[cacheKey]
	f.access = time.Now()
	c.cachedFiles[cacheKey] = f
}

func (c *cachedDownloader) cachingInfoForCacheKey(cacheKey string) CachingInfoType {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.cachedFiles[cacheKey].cachingInfo
}

func (c *cachedDownloader) setCachingInfoForCacheKey(cacheKey string, cachingInfo CachingInfoType) {
	c.lock.Lock()
	defer c.lock.Unlock()
	f := c.cachedFiles[cacheKey]
	f.cachingInfo = cachingInfo
	c.cachedFiles[cacheKey] = f
}
