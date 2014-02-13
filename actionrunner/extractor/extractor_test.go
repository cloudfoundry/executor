package extractor_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/cloudfoundry-incubator/executor/actionrunner/extractor"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extractor", func() {
	var extractor Extractor

	BeforeEach(func() {
		err := exec.Command("cp", "../fixtures/fixture.zip", "../fixtures/fixture_test.zip").Run()
		Ω(err).ShouldNot(HaveOccurred())
		extractor = New()
	})

	It("should extract zip files, generating directories, and honoring file permissions", func() {
		destination, err := extractor.Extract("../fixtures/fixture_test.zip")
		Ω(err).ShouldNot(HaveOccurred())
		defer func() {
			os.RemoveAll(destination) //Tidy up!
		}()

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
		destination, err := extractor.Extract("../fixtures/fixture_test.zip")
		Ω(err).ShouldNot(HaveOccurred())
		defer func() {
			os.RemoveAll(destination) //Tidy up!
		}()

		_, err = extractor.Extract("../fixtures/fixture_test.zip")
		Ω(err).Should(HaveOccurred())
	})
})
