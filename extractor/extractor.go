package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func Extract(src, dest string) error {
	srcType, err := mimeType(src)
	if err != nil {
		return err
	}

	switch srcType {
	case "application/zip":
		err := extractZip(src, dest)
		if err != nil {
			return err
		}
	case "application/x-gzip":
		err := extractTgz(src, dest)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported archive type: %s", srcType)
	}

	return nil
}

func mimeType(src string) (string, error) {
	fd, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer fd.Close()

	data := make([]byte, 512)

	_, err = fd.Read(data)
	if err != nil {
		return "", err
	}

	return http.DetectContentType(data), nil
}

func extractTgz(src, dest string) error {
	fd, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fd.Close()

	gReader, err := gzip.NewReader(fd)
	if err != nil {
		return err
	}
	defer gReader.Close()

	tarReader := tar.NewReader(gReader)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		err = extractArchiveFile(filepath.Join(dest, hdr.Name), tarReader, hdr.FileInfo())
		if err != nil {
			return err
		}

	}
	return nil
}

func extractZip(src, dest string) error {
	files, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer files.Close()

	for _, file := range files.File {
		err = func() error {
			readCloser, err := file.Open()
			if err != nil {
				return err
			}
			defer readCloser.Close()

			return extractArchiveFile(filepath.Join(dest, file.Name), readCloser, file.FileInfo())
		}()

		if err != nil {
			return err
		}
	}

	return nil
}

func extractArchiveFile(filePath string, input io.Reader, fileInfo os.FileInfo) error {
	err := os.MkdirAll(filepath.Dir(filePath), os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}

	if !fileInfo.IsDir() {
		fileCopy, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileInfo.Mode())
		if err != nil {
			return err
		}
		defer fileCopy.Close()

		_, err = io.Copy(fileCopy, input)
		if err != nil {
			return err
		}
	}

	return nil
}
