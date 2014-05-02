package fake_file_cache

import (
	"bytes"
	"io"
	"net/url"
)

type FakeFileCache struct {
	FetchedURL      *url.URL
	FetchedCacheKey string
	FetchedContent  []byte
	FetchError      error
}

func New() *FakeFileCache {
	return &FakeFileCache{}
}

func (c *FakeFileCache) Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	c.FetchedURL = url
	c.FetchedCacheKey = cacheKey
	return &readCloser{bytes.NewBuffer(c.FetchedContent)}, c.FetchError
}

type readCloser struct {
	buffer *bytes.Buffer
}

func (r *readCloser) Read(p []byte) (n int, err error) {
	return r.buffer.Read(p)
}

func (r *readCloser) Close() error {
	return nil
}
