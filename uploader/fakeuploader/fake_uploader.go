package fakeuploader

import (
	"errors"
	"net/url"
	"os"
)

type FakeUploader struct {
	UploadedFiles []*os.File
	UploadUrls    []*url.URL
	alwaysFail    bool
}

func (uploader *FakeUploader) Upload(sourceFile *os.File, destinationUrl *url.URL) error {
	if uploader.alwaysFail {
		return errors.New("I accidentally the upload")
	}

	uploader.UploadUrls = append(uploader.UploadUrls, destinationUrl)
	uploader.UploadedFiles = append(uploader.UploadedFiles, sourceFile)

	return nil
}

func (uploader *FakeUploader) AlwaysFail() {
	uploader.alwaysFail = true
}
