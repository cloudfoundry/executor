package extractor

import (
	"archive/zip"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/nu7hatch/gouuid"
)

type Extractor interface {
	Extract(src string) (dst string, err error)
}

type ZipExtractor struct{}

func New() Extractor {
	return &ZipExtractor{}
}

func (extractor *ZipExtractor) Extract(src string) (string, error) {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	destination := "./" + uuid.String()
	err = os.MkdirAll(destination, 0700)
	if err != nil {
		return "", err
	}

	files, err := zip.OpenReader(src)
	if err != nil {
		log.Fatal(err)
	}
	defer files.Close()

	// We loop over all files in the zip archive
	for _, file := range files.File { //ignore the index
		// If it's a directory, create the directory
		path := filepath.Join(destination, file.Name)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|os.ModePerm)
		if err != nil {
			return "", err
		}

		//Open the file for reading
		readCloser, err := file.Open()
		if err != nil {
			return "", err
		}

		// If it's a file, write out the file
		if !file.FileInfo().IsDir() {
			fileCopy, err := os.Create(path)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(fileCopy, readCloser)
			if err != nil {
				return "", err
			}
			fileCopy.Close()
		}

		//Tidy up!  It's important to do this in the loop, so that we don't run out of file descriptors on large archives.
		readCloser.Close()
	}

	os.Remove(src)
	return destination, nil
}
