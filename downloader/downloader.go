package downloader

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

	steno "github.com/cloudfoundry/gosteno"
)

const MAX_DOWNLOAD_ATTEMPTS = 3

type Downloader interface {
	Download(url *url.URL, destinationFile *os.File) (int64, error)
}

type URLDownloader struct {
	logger *steno.Logger
	client *http.Client
}

func New(timeout time.Duration, logger *steno.Logger) Downloader {
	transport := &http.Transport{
		ResponseHeaderTimeout: timeout,
	}
	client := &http.Client{
		Transport: transport,
	}

	return &URLDownloader{
		logger: logger,
		client: client,
	}
}

func (downloader *URLDownloader) Download(url *url.URL, destinationFile *os.File) (length int64, err error) {
	for attempt := 0; attempt < MAX_DOWNLOAD_ATTEMPTS; attempt++ {
		downloader.logger.Infof("downloader.attempt #%d", attempt)
		length, err = downloader.fetchToFile(url, destinationFile)
		if err == nil {
			break
		}
	}

	if err != nil {
		return 0, err
	}
	return
}

func (downloader *URLDownloader) fetchToFile(url *url.URL, destinationFile *os.File) (int64, error) {
	_, err := destinationFile.Seek(0, 0)
	if err != nil {
		return 0, err
	}

	err = destinationFile.Truncate(0)
	if err != nil {
		return 0, err
	}

	resp, err := downloader.client.Get(url.String())
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("Download failed: Status code %d", resp.StatusCode)
	}

	hash := md5.New()

	count, err := io.Copy(io.MultiWriter(destinationFile, hash), resp.Body)
	if err != nil {
		return 0, err
	}

	etagChecksum, ok := convertETagToChecksum(resp.Header.Get("ETag"))

	if ok && !bytes.Equal(etagChecksum, hash.Sum(nil)) {
		return 0, fmt.Errorf("Download failed: Checksum mismatch")
	}

	return count, nil
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
