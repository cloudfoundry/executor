package fakecacheddownloader

import (
	"bytes"
	"io"
	"net/url"
)

type FakeCachedDownloader struct {
	FetchedURL      *url.URL
	FetchedCacheKey string
	FetchedContent  []byte
	FetchError      error
}

func New() *FakeCachedDownloader {
	return &FakeCachedDownloader{}
}

func (c *FakeCachedDownloader) Fetch(url *url.URL, cacheKey string) (io.ReadCloser, error) {
	c.FetchedURL = url
	c.FetchedCacheKey = cacheKey

	if c.FetchError != nil {
		return nil, c.FetchError
	}

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
