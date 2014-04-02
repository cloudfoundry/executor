package extractor_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/extractor"
	"github.com/cloudfoundry-incubator/executor/extractor/archiver"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extractor", func() {
	var extractor Extractor

	var extractionDest string
	var extractionSrc string

	BeforeEach(func() {
		var err error

		archive, err := ioutil.TempFile("", "extractor-archive")
		Ω(err).ShouldNot(HaveOccurred())

		extractionDest, err = ioutil.TempDir("", "extracted")
		Ω(err).ShouldNot(HaveOccurred())

		extractionSrc = archive.Name()

		extractor = New()
	})

	AfterEach(func() {
		os.RemoveAll(extractionSrc)
		os.RemoveAll(extractionDest)
	})

	archiveFiles := []archiver.ArchiveFile{
		{
			Name: "some-file",
			Body: "some-file-contents",
		},
		{
			Name: "empty-dir/",
			Dir:  true,
		},
		{
			Name: "nonempty-dir/file-in-dir",
			Body: "file-in-dir-contents",
		},
		{
			Name: "legit-exe-not-a-virus.bat",
			Mode: 0755,
			Body: "rm -rf /",
		},
		{
			Name: "some-symlink",
			Link: "some-file",
		},
	}

	extractionTest := func() {
		err := extractor.Extract(extractionSrc, extractionDest)
		Ω(err).ShouldNot(HaveOccurred())

		fileContents, err := ioutil.ReadFile(filepath.Join(extractionDest, "some-file"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("some-file-contents"))

		fileContents, err = ioutil.ReadFile(filepath.Join(extractionDest, "nonempty-dir", "file-in-dir"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("file-in-dir-contents"))

		executable, err := os.Open(filepath.Join(extractionDest, "legit-exe-not-a-virus.bat"))
		Ω(err).ShouldNot(HaveOccurred())

		info, err := executable.Stat()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(info.Mode()).Should(Equal(os.FileMode(0755)))

		emptyDir, err := os.Open(filepath.Join(extractionDest, "empty-dir"))
		Ω(err).ShouldNot(HaveOccurred())

		info, err = emptyDir.Stat()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(info.IsDir()).Should(BeTrue())
	}

	Context("when the file is a zip archive", func() {
		BeforeEach(func() {
			archiver.CreateZipArchive(extractionSrc, archiveFiles)
		})

		It("extracts the ZIP's files, generating directories, and honoring file permissions", func() {
			extractionTest()
		})
	})

	Context("when the file is a tgz archive", func() {
		BeforeEach(func() {
			archiver.CreateTarGZArchive(extractionSrc, archiveFiles)
		})

		It("extracts the TGZ's files, generating directories, and honoring file permissions", func() {
			extractionTest()
		})

		It("preserves symlinks", func() {
			extractionTest()

			target, err := os.Readlink(filepath.Join(extractionDest, "some-symlink"))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(target).Should(Equal("some-file"))
		})
	})
})
