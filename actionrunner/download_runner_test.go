package actionrunner_test

import (
	"archive/zip"
	"errors"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"io/ioutil"
	"os"

	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader/fakeuploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DownloadRunner", func() {
	var (
		actions    []models.ExecutorAction
		runner     *ActionRunner
		downloader *fakedownloader.FakeDownloader
		uploader   *fakeuploader.FakeUploader
		gordon     *fake_gordon.FakeGordon
		err        error
	)

	BeforeEach(func() {
		gordon = fake_gordon.New()
		downloader = &fakedownloader.FakeDownloader{}
		uploader = &fakeuploader.FakeUploader{}
		linuxPlugin := linuxplugin.New()
		runner = New(gordon, linuxPlugin, downloader, uploader, os.TempDir(), steno.NewLogger("test-logger"))

		actions = []models.ExecutorAction{
			{
				models.DownloadAction{
					From:    "http://mr_jones",
					To:      "/Antarctica",
					Extract: false,
				},
			},
		}
	})

	JustBeforeEach(func() {
		err = runner.Run("handle-x", nil, actions)
	})

	It("should download the file from a URL", func() {
		Ω(downloader.DownloadedUrls[0].Host).To(ContainSubstring("mr_jones"))
	})

	It("should place the file in the container", func() {
		copied_file := gordon.ThingsCopiedIn()[0]
		Ω(copied_file.Handle).To(Equal("handle-x"))
		Ω(copied_file.Dst).To(Equal("/Antarctica"))
		Ω(err).ShouldNot(HaveOccurred())
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
			Ω(gordon.ThingsCopiedIn()[0].Dst).To(Equal("/Antarctica"))
			Ω(gordon.ThingsCopiedIn()[1].Dst).To(Equal("/Antarctica/directory"))
			Ω(gordon.ThingsCopiedIn()[2].Dst).To(Equal("/Antarctica/directory/second_file"))
			Ω(gordon.ThingsCopiedIn()[3].Dst).To(Equal("/Antarctica/directory/third_file"))
			Ω(gordon.ThingsCopiedIn()[4].Dst).To(Equal("/Antarctica/first_file"))
		})
	})
})
