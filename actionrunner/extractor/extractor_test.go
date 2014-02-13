package extractor_test

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extractor", func() {
	var zipFileLocation string
	var extractor Extractor

	BeforeEach(func() {
		file, err := ioutil.TempFile(os.TempDir(), "test-zip")
		Ω(err).ShouldNot(HaveOccurred())

		zipFileLocation = file.Name()

		zipWriter := zip.NewWriter(file)

		writer, err := zipWriter.CreateHeader(&zip.FileHeader{
			Name: "JerryMaguire",
		})
		Ω(err).ShouldNot(HaveOccurred())

		_, err = writer.Write([]byte("Show me the data!"))
		Ω(err).ShouldNot(HaveOccurred())

		err = zipWriter.Close()
		Ω(err).ShouldNot(HaveOccurred())

		extractor = New()
	})

	It("should download and extract zip files", func() {
		destination, err := extractor.Extract(zipFileLocation)
		Ω(err).ShouldNot(HaveOccurred())
		defer func() {
			os.RemoveAll(destination) //Tidy up!
		}()

		fileContents, err := ioutil.ReadFile(filepath.Join(destination, "JerryMaguire"))
		Ω(err).ShouldNot(HaveOccurred())

		Ω(fileContents).To(ContainSubstring("the data!"))
	})
})
