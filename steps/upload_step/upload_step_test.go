package upload_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"io/ioutil"
	"os/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/compressor/fake_compressor"
	. "github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("UploadStep", func() {
	var step sequence.Step
	var result chan error

	var uploadAction models.UploadAction
	var uploader *fake_uploader.FakeUploader
	var tempDir string
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger
	var compressor *fake_compressor.FakeCompressor

	BeforeEach(func() {
		var err error

		result = make(chan error)

		uploadAction = models.UploadAction{
			To:       "http://mr_jones",
			From:     "/Antarctica",
			Compress: false,
		}

		uploader = &fake_uploader.FakeUploader{}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

		logger = steno.NewLogger("test-logger")
		compressor = &fake_compressor.FakeCompressor{}
	})

	JustBeforeEach(func() {
		step = New(
			"some-container-handle",
			uploadAction,
			uploader,
			compressor,
			tempDir,
			wardenClient,
			logger,
		)
	})

	Describe("Perform", func() {
		It("uploads the file to the given URL", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(uploader.UploadUrls).ShouldNot(BeEmpty())
			Ω(uploader.UploadUrls[0].Host).To(ContainSubstring("mr_jones"))

			Ω(uploader.UploadedFileLocations).ShouldNot(BeEmpty())
			Ω(uploader.UploadedFileLocations[0]).To(ContainSubstring(tempDir))
		})

		It("copies the file out of the container", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			currentUser, err := user.Current()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.ThingsCopiedOut()).ShouldNot(BeEmpty())

			copiedFile := wardenClient.ThingsCopiedOut()[0]
			Ω(copiedFile.Handle).Should(Equal("some-container-handle"))
			Ω(copiedFile.Src).To(Equal("/Antarctica"))
			Ω(copiedFile.Owner).To(Equal(currentUser.Username))
		})

		Context("when there is an error copying the file out", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				wardenClient.SetCopyOutErr(disaster)
			})

			It("returns the error", func() {
				err := step.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when there is an error uploading", func() {
			BeforeEach(func() {
				uploader.AlwaysFail() //and bring shame and dishonor to your house
			})

			It("fails", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when compress is set to true", func() {
			BeforeEach(func() {
				uploadAction = models.UploadAction{
					To:       "http://mr_jones",
					From:     "/Antarctica",
					Compress: true,
				}
			})

			It("compresses the src to a tgz file", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(compressor.Src).Should(ContainSubstring(tempDir))
				Ω(compressor.Dest).Should(ContainSubstring(tempDir))
			})

			Context("and compressing fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					compressor.CompressError = disaster
				})

				It("returns the error", func() {
					err := step.Perform()
					Ω(err).Should(Equal(disaster))
				})
			})
		})
	})
})
