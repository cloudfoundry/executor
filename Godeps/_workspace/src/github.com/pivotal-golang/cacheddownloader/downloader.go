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

func (downloader *Downloader) Download(url *url.URL, destinationFile *os.File, etagIn string) (didDownload bool, length int64, etagOut string, err error) {
	for attempt := 0; attempt < MAX_DOWNLOAD_ATTEMPTS; attempt++ {
		didDownload, length, etagOut, err = downloader.fetchToFile(url, destinationFile, etagIn)
		if err == nil {
			break
		}
	}

	if err != nil {
		return false, 0, "", err
	}
	return
}

func (downloader *Downloader) fetchToFile(url *url.URL, destinationFile *os.File, etagIn string) (bool, int64, string, error) {
	_, err := destinationFile.Seek(0, 0)
	if err != nil {
		return false, 0, "", err
	}

	err = destinationFile.Truncate(0)
	if err != nil {
		return false, 0, "", err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return false, 0, "", err
	}

	if etagIn != "" {
		req.Header.Add("If-None-Match", etagIn)
	}
	resp, err := downloader.client.Do(req)
	if err != nil {
		return false, 0, "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return false, 0, "", fmt.Errorf("Download failed: Status code %d", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusNotModified {
		return false, 0, "", nil
	}

	hash := md5.New()

	count, err := io.Copy(io.MultiWriter(destinationFile, hash), resp.Body)
	if err != nil {
		return false, 0, "", err
	}

	etagOut := resp.Header.Get("ETag")

	etagChecksum, ok := convertETagToChecksum(etagOut)

	if ok && !bytes.Equal(etagChecksum, hash.Sum(nil)) {
		return false, 0, "", fmt.Errorf("Download failed: Checksum mismatch")
	}

	return true, count, etagOut, nil
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
