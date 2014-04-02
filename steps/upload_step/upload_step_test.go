package upload_step_test

import (
	"errors"
	"io/ioutil"
	"os/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/compressor/fake_compressor"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/steps/upload_step"
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
	var fakeStreamer *fake_log_streamer.FakeLogStreamer

	BeforeEach(func() {
		var err error

		result = make(chan error)

		uploader = &fake_uploader.FakeUploader{}
		uploader.UploadSize = 1024
		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

		logger = steno.NewLogger("test-logger")
		compressor = &fake_compressor.FakeCompressor{}

		fakeStreamer = fake_log_streamer.New()
	})

	Describe("Perform", func() {
		var stepErr error

		BeforeEach(func() {
			uploadAction = models.UploadAction{
				Name:     "Mr. Jones",
				To:       "http://mr_jones",
				From:     "/Antarctica",
				Compress: false,
			}
		})

		JustBeforeEach(func() {
			step = New(
				"some-container-handle",
				uploadAction,
				uploader,
				compressor,
				tempDir,
				wardenClient,
				fakeStreamer,
				logger,
			)

			stepErr = step.Perform()
		})

		Context("when successful", func() {
			It("does not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("uploads the file to the given URL", func() {
				Ω(uploader.UploadUrls).ShouldNot(BeEmpty())
				Ω(uploader.UploadUrls[0].Host).To(ContainSubstring("mr_jones"))
			})

			It("uploads the correct file location", func() {
				Ω(uploader.UploadedFileLocations).ShouldNot(BeEmpty())
				Ω(uploader.UploadedFileLocations[0]).To(ContainSubstring(tempDir))
			})

			It("copies the file out of the container", func() {
				currentUser, err := user.Current()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(wardenClient.ThingsCopiedOut()).ShouldNot(BeEmpty())

				copiedFile := wardenClient.ThingsCopiedOut()[0]
				Ω(copiedFile.Handle).Should(Equal("some-container-handle"))
				Ω(copiedFile.Src).To(Equal("/Antarctica"))
				Ω(copiedFile.Owner).To(Equal(currentUser.Username))
			})

			It("streams an upload message", func() {
				Ω(fakeStreamer.StreamedStdout).Should(ContainSubstring("Uploading Mr. Jones"))
			})

			It("streams the upload filesize", func() {
				Ω(fakeStreamer.StreamedStdout).Should(ContainSubstring("(1K)"))
			})

			It("does not stream an error", func() {
				Ω(fakeStreamer.StreamedStderr).Should(Equal(""))
			})
		})

		Context("when there is an error parsing the upload url", func() {
			BeforeEach(func() {
				uploadAction.To = "foo/bar"
			})

			It("returns an error", func() {
				Ω(stepErr).Should(HaveOccurred())
			})

			It("loggregates a message to STDERR", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Uploading Mr. Jones failed"))
			})
		})

		Context("when there is an error copying the file out", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				wardenClient.SetCopyOutErr(disaster)
			})

			It("returns the error", func() {
				Ω(stepErr).Should(Equal(disaster))
			})

			It("loggregates a message to STDERR stream", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Uploading Mr. Jones failed"))
			})
		})

		Context("when there is an error uploading", func() {
			BeforeEach(func() {
				uploader.AlwaysFail() //and bring shame and dishonor to your house
			})

			It("fails", func() {
				Ω(stepErr).Should(HaveOccurred())
			})

			It("loggregates a message to STDERR stream", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Uploading Mr. Jones failed"))
			})
		})

		Context("when compress is set to true", func() {
			BeforeEach(func() {
				uploadAction.Compress = true
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

				It("loggregates a message to STDERR stream", func() {
					Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Uploading Mr. Jones failed"))
				})
			})
		})
	})
})
