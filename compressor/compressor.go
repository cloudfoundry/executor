package compressor

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

type Compressor interface {
	Compress(src string, dest string) error
}

func New() Compressor {
	return &realCompressor{}
}

type realCompressor struct{}

func (compressor *realCompressor) Compress(src string, dest string) error {
	absPath, err := filepath.Abs(src)
	if err != nil {
		return err
	}

	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// file write
	fw, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer fw.Close()

	gw := gzip.NewWriter(fw)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return compressRecursively(file, absPath, tw)
}

func compressRecursively(file *os.File, relativeFrom string, tw *tar.Writer) error {
	info, err := os.Lstat(file.Name())
	if err != nil {
		return err
	}

	if info.IsDir() {
		files, err := file.Readdir(0)
		if err != nil {
			return err
		}

		for _, info := range files {
			subName := filepath.Join(file.Name(), info.Name())

			subFile, err := os.Open(subName)
			if err != nil {
				return err
			}

			err = compressRecursively(subFile, relativeFrom, tw)
			if err != nil {
				return err
			}
		}
	} else {
		err = addFileToTar(file, info, relativeFrom, tw)
		if err != nil {
			return err
		}
	}

	return nil
}

func addFileToTar(file *os.File, info os.FileInfo, relativeFrom string, tw *tar.Writer) error {
	link, err := os.Readlink(file.Name())
	if err != nil {
		link = ""
	}

	h, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}

	relative, err := filepath.Rel(relativeFrom, file.Name())
	if err != nil {
		return err
	}

	if relative == "." {
		relative = info.Name()
	}

	h.Name = relative

	err = tw.WriteHeader(h)
	if err != nil {
		return err
	}

	if link == "" {
		_, err := io.Copy(tw, file)
		if err != nil {
			return err
		}
	}

	return nil
}
