package compressor_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	// "time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/executor/compressor"
	"github.com/cloudfoundry-incubator/executor/extractor"
)

func retrieveFilePaths(dir string) (results []string) {
	err := filepath.Walk(dir, func(singlePath string, info os.FileInfo, err error) error {
		relative, err := filepath.Rel(dir, singlePath)
		Ω(err).ShouldNot(HaveOccurred())

		results = append(results, relative)
		return nil
	})

	Ω(err).NotTo(HaveOccurred())

	return results
}

var _ = Describe("Compressor", func() {
	var compressor Compressor
	var tmpDir string
	var extracticator extractor.Extractor
	var err error

	BeforeEach(func() {
		compressor = New()
		extracticator = extractor.New()

		tmpDir, err = ioutil.TempDir("", "")
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// os.RemoveAll(tmpDir)
	})

	It("compresses the src file to dest file", func() {
		srcFile := filepath.Join("..", "fixtures", "file_to_compress")

		destFile := filepath.Join(tmpDir, "compress-dst.tgz")

		err = compressor.Compress(srcFile, destFile)
		Ω(err).NotTo(HaveOccurred())

		finalReadingDir, err := ioutil.TempDir(tmpDir, "final")
		Ω(err).NotTo(HaveOccurred())

		err = extracticator.Extract(destFile, finalReadingDir)
		Ω(err).NotTo(HaveOccurred())

		expectedContent, err := ioutil.ReadFile(srcFile)
		Ω(err).NotTo(HaveOccurred())

		actualContent, err := ioutil.ReadFile(filepath.Join(finalReadingDir, "file_to_compress"))
		Ω(err).NotTo(HaveOccurred())
		Ω(actualContent).Should(Equal(expectedContent))
	})

	It("compresses the src path recursively to dest file", func() {
		srcDir := filepath.Join("..", "fixtures", "folder_to_compress")

		destFile := filepath.Join(tmpDir, "compress-dst.tgz")

		err = compressor.Compress(srcDir, destFile)
		Ω(err).ShouldNot(HaveOccurred())

		finalReadingDir, err := ioutil.TempDir(tmpDir, "final")
		Ω(err).ShouldNot(HaveOccurred())

		// println("I AM HERE: ", tmpDir)
		// time.Sleep(time.Hour)
		err = extracticator.Extract(destFile, finalReadingDir)
		Ω(err).ShouldNot(HaveOccurred())

		expectedFilePaths := retrieveFilePaths(srcDir)
		actualFilePaths := retrieveFilePaths(finalReadingDir)

		Ω(actualFilePaths).To(Equal(expectedFilePaths))
	})

	It("returns error if any", func() {

	})
})
