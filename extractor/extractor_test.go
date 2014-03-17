package extractor_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry/gofileutils/fileutils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extractor", func() {
	var extractionDest string
	var extractionSrc string
	var tempDir string
	var archiveFixture string

	JustBeforeEach(func() {
		var err error

		tempDir, err = ioutil.TempDir("", "extractor-fixture")
		Ω(err).ShouldNot(HaveOccurred())

		extractionSrc = filepath.Join(tempDir, archiveFixture)

		err = fileutils.CopyPathToPath(filepath.Join("..", "fixtures", archiveFixture), extractionSrc)
		Ω(err).ShouldNot(HaveOccurred())

		extractionDest, err = ioutil.TempDir(tempDir, "extracted")
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	var extractionTest = func() {
		err := Extract(extractionSrc, extractionDest)
		Ω(err).ShouldNot(HaveOccurred())

		fileContents, err := ioutil.ReadFile(filepath.Join(extractionDest, "fixture", "file"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("I am a file"))

		fileContents, err = ioutil.ReadFile(filepath.Join(extractionDest, "fixture", "iamadirectory", "another_file"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("I am another file"))

		f, err := os.Open(filepath.Join(extractionDest, "fixture", "iamadirectory", "supervirus.exe"))
		Ω(err).ShouldNot(HaveOccurred())

		info, err := f.Stat()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(info.Mode()).Should(Equal(os.FileMode(0755)))
	}

	Context("when the file is a zip archive", func() {
		BeforeEach(func() {
			archiveFixture = "fixture.zip"
		})

		It("extracts the ZIP's files, generating directories, and honoring file permissions", func() {
			extractionTest()
		})
	})

	Context("when the file is a tgz archive", func() {
		BeforeEach(func() {
			archiveFixture = "fixture.tgz"
		})

		It("extracts the TGZ's files, generating directories, and honoring file permissions", func() {
			extractionTest()
		})
	})
})
