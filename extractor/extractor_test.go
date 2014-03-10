package extractor_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/extractor"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extractor", func() {
	var destination string
	var fixturetmp string
	var fixture string

	BeforeEach(func() {
		var err error

		fixturetmp, err = ioutil.TempDir("", "extractor-fixture")
		Ω(err).ShouldNot(HaveOccurred())

		fixture = filepath.Join(fixturetmp, "fixture.zip")

		err = exec.Command("cp", "../fixtures/fixture.zip", fixture).Run()
		Ω(err).ShouldNot(HaveOccurred())

		destination, err = ioutil.TempDir(os.TempDir(), "extracted")
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(destination)
		os.RemoveAll(fixturetmp)
	})

	It("should extract zip files, generating directories, and honoring file permissions", func() {
		err := Extract(fixture, destination)
		Ω(err).ShouldNot(HaveOccurred())

		fileContents, err := ioutil.ReadFile(filepath.Join(destination, "fixture", "file"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("I am a file"))

		fileContents, err = ioutil.ReadFile(filepath.Join(destination, "fixture", "iamadirectory", "another_file"))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(string(fileContents)).Should(Equal("I am another file"))

		f, err := os.Open(filepath.Join(destination, "fixture", "iamadirectory", "supervirus.exe"))
		Ω(err).ShouldNot(HaveOccurred())

		info, err := f.Stat()
		Ω(err).ShouldNot(HaveOccurred())

		Ω(info.Mode()).Should(Equal(os.FileMode(0755)))
	})

	It("should delete the zip file when its done", func() {
		err := Extract(fixture, destination)
		Ω(err).ShouldNot(HaveOccurred())

		err = Extract(fixture, destination)
		Ω(err).Should(HaveOccurred())
	})
})
