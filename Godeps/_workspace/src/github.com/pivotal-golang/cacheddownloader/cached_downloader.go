package cacheddownloader

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
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
}

func New(cachedPath string, uncachedPath string, maxSizeInBytes int64, downloadTimeout time.Duration) *cachedDownloader {
	os.RemoveAll(cachedPath)
	os.MkdirAll(cachedPath, 0770)
	return &cachedDownloader{
		downloader:   NewDownloader(downloadTimeout),
		uncachedPath: uncachedPath,
		cache:        NewCache(cachedPath, maxSizeInBytes),
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
	download, err := c.downloadFile(url, "uncached", CachingInfoType{})

	// Use os.RemoveAll because on windows, os.Remove will remove
	// the dir of the file if the file doesn't exist and the dir of the file is
	// empty.
	defer os.RemoveAll(download.path)
	if err != nil {
		return nil, err
	}

	return tempFileCloser(download.path)
}

func (c *cachedDownloader) fetchCachedFile(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	c.cache.RecordAccess(cacheKey)

	download, err := c.downloadFile(url, cacheKey, c.cache.Info(cacheKey))

	// Use os.RemoveAll because on windows, os.Remove will remove
	// the dir of the file if the file doesn't exist and the dir of the file is
	// empty.
	defer os.RemoveAll(download.path)
	if err != nil {
		return nil, err
	}

	if download.matchesCache {
		return c.cache.Get(cacheKey)
	} else {
		if download.isCachable() {
			movedToCache, err := c.cache.Add(cacheKey, download.path, download.size, download.cachingInfo)
			if err != nil {
				return nil, err
			}

			if movedToCache {
				return c.cache.Get(cacheKey)
			} else {
				return tempFileCloser(download.path)
			}
		} else {
			c.cache.RemoveEntry(cacheKey)
			return tempFileCloser(download.path)
		}
	}
}

func tempFileCloser(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return NewFileCloser(f, func(path string) {
		os.RemoveAll(path)
	}), nil
}

type download struct {
	matchesCache bool
	path         string
	size         int64
	cachingInfo  CachingInfoType
}

func (d download) isCachable() bool {
	return d.cachingInfo.ETag != "" || d.cachingInfo.LastModified != ""
}

func (c *cachedDownloader) downloadFile(url *url.URL, name string, cachingInfo CachingInfoType) (download, error) {
	downloadedFile, err := ioutil.TempFile(c.uncachedPath, name+"-")
	if err != nil {
		return download{}, err
	}

	didDownload, size, cachingInfo, err := c.downloader.Download(url, downloadedFile, cachingInfo)
	downloadedFile.Close()
	if err != nil {
		os.RemoveAll(downloadedFile.Name())
		return download{}, err
	}

	return download{
		matchesCache: !didDownload,
		path:         downloadedFile.Name(),
		size:         size,
		cachingInfo:  cachingInfo,
	}, nil
}
