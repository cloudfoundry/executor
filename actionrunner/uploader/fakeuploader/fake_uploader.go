package fakeuploader

import (
	"errors"
	"net/url"
)

type FakeUploader struct {
	UploadedFileLocations []string
	UploadUrls            []*url.URL
	alwaysFail            bool
}

func (uploader *FakeUploader) Upload(fileLocation string, destinationUrl *url.URL) error {
	if uploader.alwaysFail {
		return errors.New("I accidentally the upload")
	}

	uploader.UploadUrls = append(uploader.UploadUrls, destinationUrl)
	uploader.UploadedFileLocations = append(uploader.UploadedFileLocations, fileLocation)

	return nil
}

func (uploader *FakeUploader) AlwaysFail() {
	uploader.alwaysFail = true
}
