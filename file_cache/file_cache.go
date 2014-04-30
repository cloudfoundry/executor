package file_cache

import (
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/executor/downloader"
)

type FileCache interface {
	Fetch(url string, cacheKey string) (io.ReadCloser, error)
}

type Cache struct {
	basePath       string
	maxSizeInBytes int
	downloader     downloader.Downloader
}

func New(basePath string, maxSizeInBytes int, downloader downloader.Downloader) *Cache {
	return &Cache{
		basePath:       basePath,
		maxSizeInBytes: maxSizeInBytes,
		downloader:     downloader,
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

	return &SingleUseFile{destinationFile}, err
}

func (c *Cache) fetchCachedFile(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	if c.hasCachedFileForCacheKey(cacheKey) {
		fileInfo, err := os.Stat(c.pathForCacheKey(cacheKey))
		if err != nil {
			return nil, err
		}

		modified, err := c.downloader.ModifiedSince(url, fileInfo.ModTime())
		if err != nil {
			return nil, err
		}

		if !modified {
			return os.Open(c.pathForCacheKey(cacheKey))
		}
	}

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

	return destinationFile, err
}

func (c *Cache) pathForCacheKey(cacheKey string) string {
	return filepath.Join(c.basePath, cacheKey)
}

func (c *Cache) hasCachedFileForCacheKey(cacheKey string) bool {
	_, err := os.Stat(c.pathForCacheKey(cacheKey))
	return err == nil
}

type SingleUseFile struct {
	file *os.File
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
