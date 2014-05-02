package fakecacheddownloader

import (
	"bytes"
	"io"
	"net/url"
)

type FakeCachedDownloader struct {
	FetchedURL     *url.URL
	FetchedCache   bool
	FetchedContent []byte
	FetchError     error
}

func New() *FakeCachedDownloader {
	return &FakeCachedDownloader{}
}

func (c *FakeCachedDownloader) Fetch(url *url.URL, cache bool) (io.ReadCloser, error) {
	c.FetchedURL = url
	c.FetchedCache = cache
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
