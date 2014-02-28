package actionrunner_test

import (
	"archive/zip"
	"errors"
	"io/ioutil"
	"os"

	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DownloadRunner", func() {
	var (
		actions []models.ExecutorAction
		err     error
	)

	BeforeEach(func() {
		actions = []models.ExecutorAction{
			{
				models.DownloadAction{
					From:    "http://mr_jones",
					To:      "/tmp/Antarctica",
					Extract: false,
				},
			},
		}
	})

	JustBeforeEach(func() {
		_, err = runner.Run("handle-x", nil, actions)
	})

	It("should download the file from a URL", func() {
		Ω(downloader.DownloadedUrls[0].Host).To(ContainSubstring("mr_jones"))
	})

	It("should place the file in the container", func() {
		copied_file := gordon.ThingsCopiedIn()[0]
		Ω(copied_file.Handle).To(Equal("handle-x"))
		Ω(copied_file.Dst).To(Equal("/tmp/Antarctica"))
		Ω(err).ShouldNot(HaveOccurred())
	})

	It("should create the parent of destination directory", func() {
		scriptThatRun := gordon.ScriptsThatRan()[0]
		Ω(scriptThatRun.Handle).To(Equal("handle-x"))
		Ω(scriptThatRun.Script).To(Equal("mkdir -p /tmp"))
	})

	Context("when there is an error copying the file in", func() {
		BeforeEach(func() {
			gordon.SetCopyInErr(errors.New("no room in the copy inn"))
		})

		It("should return said error", func() {
			Ω(err).Should(Equal(errors.New("no room in the copy inn")))
		})
	})

	Context("when there is an error downloading", func() {
		BeforeEach(func() {
			downloader.AlwaysFail() //and bring shame and dishonor to your house
		})

		It("should return the error", func() {
			Ω(err).ToNot(BeNil())
		})
	})

	Context("when the file needs extraction", func() {
		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.DownloadAction{
						From:    "http://mr_jones",
						To:      "/Antarctica",
						Extract: true,
					},
				},
			}

			file, err := ioutil.TempFile(os.TempDir(), "test-zip")
			Ω(err).ShouldNot(HaveOccurred())

			zipWriter := zip.NewWriter(file)

			Ω(err).ShouldNot(HaveOccurred())
			firstFileWriter, _ := zipWriter.Create("first_file")
			firstFileWriter.Write([]byte("I"))
			secondFileWriter, _ := zipWriter.Create("directory/second_file")
			secondFileWriter.Write([]byte("love"))
			thirdFileWriter, _ := zipWriter.Create("directory/third_file")
			thirdFileWriter.Write([]byte("peaches"))

			err = zipWriter.Close()
			Ω(err).ShouldNot(HaveOccurred())

			downloader.SourceFile = file
		})

		It("should download the zipped file and send the contents to the container", func() {
			Ω(gordon.ScriptsThatRan)
			Ω(gordon.ThingsCopiedIn()[0].Dst).To(Equal("/Antarctica/"))
		})
	})
})
