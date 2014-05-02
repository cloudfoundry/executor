package cacheddownloader

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const MAX_DOWNLOAD_ATTEMPTS = 3

type Downloader struct {
	client *http.Client
}

func NewDownloader(timeout time.Duration) *Downloader {
	transport := &http.Transport{
		ResponseHeaderTimeout: timeout,
	}
	client := &http.Client{
		Transport: transport,
	}

	return &Downloader{
		client: client,
	}
}

func (downloader *Downloader) Download(url *url.URL, destinationFile *os.File, cachingInfoIn CachingInfoType) (didDownload bool, length int64, cachingInfoOut CachingInfoType, err error) {
	for attempt := 0; attempt < MAX_DOWNLOAD_ATTEMPTS; attempt++ {
		didDownload, length, cachingInfoOut, err = downloader.fetchToFile(url, destinationFile, cachingInfoIn)
		if err == nil {
			break
		}
	}

	if err != nil {
		return false, 0, CachingInfoType{}, err
	}
	return
}

func (downloader *Downloader) fetchToFile(url *url.URL, destinationFile *os.File, cachingInfoIn CachingInfoType) (bool, int64, CachingInfoType, error) {
	_, err := destinationFile.Seek(0, 0)
	if err != nil {
		return false, 0, CachingInfoType{}, err
	}

	err = destinationFile.Truncate(0)
	if err != nil {
		return false, 0, CachingInfoType{}, err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return false, 0, CachingInfoType{}, err
	}

	if cachingInfoIn.ETag != "" {
		req.Header.Add("If-None-Match", cachingInfoIn.ETag)
	}
	if cachingInfoIn.LastModified != "" {
		req.Header.Add("If-Modified-Since", cachingInfoIn.LastModified)
	}

	resp, err := downloader.client.Do(req)
	if err != nil {
		return false, 0, CachingInfoType{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return false, 0, CachingInfoType{}, fmt.Errorf("Download failed: Status code %d", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusNotModified {
		return false, 0, CachingInfoType{}, nil
	}

	hash := md5.New()

	count, err := io.Copy(io.MultiWriter(destinationFile, hash), resp.Body)
	if err != nil {
		return false, 0, CachingInfoType{}, err
	}

	cachingInfoOut := CachingInfoType{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	etagChecksum, ok := convertETagToChecksum(cachingInfoOut.ETag)

	if ok && !bytes.Equal(etagChecksum, hash.Sum(nil)) {
		return false, 0, CachingInfoType{}, fmt.Errorf("Download failed: Checksum mismatch")
	}

	return true, count, cachingInfoOut, nil
}

// convertETagToChecksum returns true if ETag is a valid MD5 hash, so a checksum action was intended.
// See here for our motivation: http://docs.aws.amazon.com/AmazonS3/latest/API/RESTCommonResponseHeaders.html
func convertETagToChecksum(etag string) ([]byte, bool) {
	etag = strings.Trim(etag, `"`)

	if len(etag) != 32 {
		return nil, false
	}

	c, err := hex.DecodeString(etag)
	if err != nil {
		return nil, false
	}

	return c, true
}
