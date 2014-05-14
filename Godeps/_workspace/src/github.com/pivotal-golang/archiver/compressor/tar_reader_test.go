package compressor_test

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/pivotal-golang/archiver/compressor"
)

var _ = Describe("Tgz Compressor", func() {
	var srcPath string
	var tarReader io.Reader

	BeforeEach(func() {
		dir, err := ioutil.TempDir("", "archive-dir")
		Ω(err).ShouldNot(HaveOccurred())

		err = os.Mkdir(filepath.Join(dir, "outer-dir"), 0755)
		Ω(err).ShouldNot(HaveOccurred())

		err = os.Mkdir(filepath.Join(dir, "outer-dir", "inner-dir"), 0755)
		Ω(err).ShouldNot(HaveOccurred())

		innerFile, err := os.Create(filepath.Join(dir, "outer-dir", "inner-dir", "some-file"))
		Ω(err).ShouldNot(HaveOccurred())

		_, err = innerFile.Write([]byte("sup"))
		Ω(err).ShouldNot(HaveOccurred())

		srcPath = filepath.Join(dir, "outer-dir")
	})

	JustBeforeEach(func() {
		tarReader = NewTarReader(srcPath)
	})

	It("returns a reader representing a .tar stream", func() {
		reader := tar.NewReader(tarReader)

		header, err := reader.Next()
		Ω(err).ShouldNot(HaveOccurred())
		Ω(header.Name).Should(Equal("outer-dir/"))
		Ω(header.FileInfo().IsDir()).Should(BeTrue())

		header, err = reader.Next()
		Ω(err).ShouldNot(HaveOccurred())
		Ω(header.Name).Should(Equal("outer-dir/inner-dir/"))
		Ω(header.FileInfo().IsDir()).Should(BeTrue())

		header, err = reader.Next()
		Ω(err).ShouldNot(HaveOccurred())
		Ω(header.Name).Should(Equal("outer-dir/inner-dir/some-file"))
		Ω(header.FileInfo().IsDir()).Should(BeFalse())

		contents, err := ioutil.ReadAll(reader)
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(contents)).Should(Equal("sup"))
	})

	Context("when the tar is fully read", func() {
		It("returns EOF for later reads", func() {
			_, err := io.Copy(ioutil.Discard, tarReader)
			Ω(err).ShouldNot(HaveOccurred())

			buf := make([]byte, 16)
			_, err = tarReader.Read(buf)
			Ω(err).Should(Equal(io.EOF))
		})
	})

	Context("when there is no file at the given path", func() {
		BeforeEach(func() {
			srcPath = filepath.Join(srcPath, "barf")
		})

		It("returns an error", func() {
			buf := new(bytes.Buffer)

			_, err := io.Copy(buf, tarReader)
			Ω(err).Should(HaveOccurred())
			Ω(err).Should(BeAssignableToTypeOf(&os.PathError{}))
		})
	})
})
