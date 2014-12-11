package uploader

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/pivotal-golang/lager"
)

var ErrUploadCancelled = errors.New("upload cancelled")

type Uploader interface {
	Upload(fileLocation string, destinationUrl *url.URL, cancel <-chan struct{}) (int64, error)
}

type URLUploader struct {
	httpClient *http.Client
	transport  *http.Transport
	logger     lager.Logger
}

func New(timeout time.Duration, skipSSLVerification bool, logger lager.Logger) Uploader {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipSSLVerification,
			MinVersion:         tls.VersionTLS10,
		},
		ResponseHeaderTimeout: timeout,
	}

	httpClient := &http.Client{
		Transport: transport,
	}

	return &URLUploader{
		httpClient: httpClient,
		transport:  transport,
		logger:     logger.Session("URLUploader"),
	}
}

func (uploader *URLUploader) Upload(fileLocation string, url *url.URL, cancel <-chan struct{}) (int64, error) {
	var resp *http.Response
	var err error
	var uploadedBytes int64

	logger := uploader.logger.WithData(lager.Data{
		"fileLocation": fileLocation,
	})

	for attempt := 0; attempt < 3; attempt++ {
		var request *http.Request
		var sourceFile *os.File
		var fileInfo os.FileInfo

		logger.Info("attempt", lager.Data{
			"attempt": attempt,
		})

		sourceFile, err = os.Open(fileLocation)
		if err != nil {
			logger.Error("failed-open", err)
			return 0, err
		}

		fileInfo, err = sourceFile.Stat()
		if err != nil {
			logger.Error("failed-stat", err)
			return 0, err
		}

		contentHash := md5.New()
		_, err = io.Copy(contentHash, sourceFile)
		if err != nil {
			logger.Error("failed-copy", err)
			return 0, err
		}

		_, err = sourceFile.Seek(0, 0)
		if err != nil {
			logger.Error("failed-seek", err)
			return 0, err
		}

		request, err = http.NewRequest("POST", url.String(), sourceFile)
		if err != nil {
			logger.Error("somehow-failed-to-create-request", err)
			return 0, err
		}

		finished := make(chan struct{})

		wg := new(sync.WaitGroup)
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-cancel:
				uploader.transport.CancelRequest(request)
			case <-finished:
			}
		}()

		uploadedBytes = fileInfo.Size()
		request.ContentLength = uploadedBytes
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(contentHash.Sum(nil)))

		resp, err = uploader.httpClient.Do(request)

		close(finished)
		wg.Wait()

		if err == nil {
			defer resp.Body.Close()
			break
		}

		select {
		case <-cancel:
			logger.Info("canceled-upload")
			return 0, ErrUploadCancelled
		default:
		}
	}

	if err != nil {
		logger.Error("failed-upload", err)
		return 0, err
	}

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("Upload failed: Status code %d", resp.StatusCode)
	}

	return int64(uploadedBytes), nil
}
