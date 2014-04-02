package fake_uploader

import (
	"errors"
	"net/url"
)

type FakeUploader struct {
	UploadedFileLocations []string
	UploadUrls            []*url.URL
	UploadSize            int64
	alwaysFail            bool
}

func (uploader *FakeUploader) Upload(fileLocation string, destinationUrl *url.URL) (int64, error) {
	if uploader.alwaysFail {
		return 0, errors.New("I accidentally the upload")
	}

	uploader.UploadUrls = append(uploader.UploadUrls, destinationUrl)
	uploader.UploadedFileLocations = append(uploader.UploadedFileLocations, fileLocation)

	return uploader.UploadSize, nil
}

func (uploader *FakeUploader) AlwaysFail() {
	uploader.alwaysFail = true
}
