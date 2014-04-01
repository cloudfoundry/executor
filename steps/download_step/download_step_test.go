package download_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/downloader/fake_downloader"
	"github.com/cloudfoundry-incubator/executor/extractor/fake_extractor"
	"github.com/cloudfoundry-incubator/executor/linux_plugin"
	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/steps/download_step"
)

var _ = Describe("DownloadAction", func() {
	var step sequence.Step
	var result chan error

	var downloadAction models.DownloadAction
	var downloader *fake_downloader.FakeDownloader
	var extractor *fake_extractor.FakeExtractor
	var tempDir string
	var backendPlugin *linux_plugin.LinuxPlugin
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger
	var fakeStreamer *fake_log_streamer.FakeLogStreamer

	BeforeEach(func() {
		var err error

		result = make(chan error)

		downloader = &fake_downloader.FakeDownloader{}
		extractor = &fake_extractor.FakeExtractor{}

		tempDir, err = ioutil.TempDir("", "download-action-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

		backendPlugin = linux_plugin.New()

		logger = steno.NewLogger("test-logger")

		fakeStreamer = fake_log_streamer.New()
	})

	Describe("Perform", func() {
		var stepErr error

		BeforeEach(func() {
			downloadAction = models.DownloadAction{
				Name:    "Mr. Jones",
				From:    "http://mr_jones",
				To:      "/tmp/Antarctica",
				Extract: false,
			}
		})

		JustBeforeEach(func() {
			step = New(
				"some-container-handle",
				downloadAction,
				downloader,
				extractor,
				tempDir,
				backendPlugin,
				wardenClient,
				fakeStreamer,
				logger,
			)

			stepErr = step.Perform()
		})

		Context("when extract is false", func() {
			It("should not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("downloads the file from the given URL", func() {
				Ω(downloader.DownloadedUrls).ShouldNot(BeEmpty())
				Ω(downloader.DownloadedUrls[0].Host).To(ContainSubstring("mr_jones"))
			})

			It("places the file in the container", func() {
				Ω(wardenClient.ThingsCopiedIn()).ShouldNot(BeEmpty())

				copiedFile := wardenClient.ThingsCopiedIn()[0]
				Ω(copiedFile.Handle).To(Equal("some-container-handle"))
				Ω(copiedFile.Dst).To(Equal("/tmp/Antarctica"))
			})

			It("streams a download message", func() {
				Ω(fakeStreamer.StreamedStdout).Should(ContainSubstring("Downloading Mr. Jones"))
			})

			It("does not stream an error", func() {
				Ω(fakeStreamer.StreamedStderr).Should(Equal(""))
			})
		})

		Context("when extract is true", func() {
			BeforeEach(func() {
				downloadAction.Extract = true
			})

			It("should not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("uses the specified extractor", func() {
				src, dest := extractor.ExtractInput()
				Ω(src).To(ContainSubstring(tempDir))
				Ω(dest).To(ContainSubstring(tempDir))
			})

			It("places the file in the container under the destination", func() {
				Ω(wardenClient.ThingsCopiedIn()).ShouldNot(BeEmpty())
				copiedFile := wardenClient.ThingsCopiedIn()[0]
				Ω(copiedFile.Handle).To(Equal("some-container-handle"))
				Ω(copiedFile.Src).To(ContainSubstring(tempDir))
				Ω(copiedFile.Dst).To(Equal("/tmp/Antarctica/"))
			})

			Context("when there is an error extracting the file", func() {
				var expectedErr = errors.New("extraction failed")
				BeforeEach(func() {
					extractor.SetExtractOutput(expectedErr)
				})

				It("returns an error", func() {
					Ω(stepErr).Should(Equal(expectedErr))
				})

				It("loggregates a message to STDERR", func() {
					Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Downloading Mr. Jones failed"))
				})
			})
		})

		Context("when there is an error parsing the download url", func() {
			BeforeEach(func() {
				downloadAction.From = "foo/bar"
			})

			It("returns an error", func() {
				Ω(stepErr).Should(HaveOccurred())
			})

			It("loggregates a message to STDERR", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Downloading Mr. Jones failed"))
			})
		})

		Context("when there is an error downloading the file", func() {
			BeforeEach(func() {
				downloader.AlwaysFail()
			})

			It("returns an error", func() {
				Ω(stepErr).Should(HaveOccurred())
			})

			It("loggregates a message to STDERR", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Downloading Mr. Jones failed"))
			})
		})

		Context("when there is an error copying the file into the container", func() {
			var expectedErr = errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.SetCopyInErr(expectedErr)
			})

			It("returns an error", func() {
				Ω(stepErr).Should(Equal(expectedErr))
			})

			It("loggregates a message to STDERR", func() {
				Ω(fakeStreamer.StreamedStderr).Should(ContainSubstring("Downloading Mr. Jones failed"))
			})
		})
	})
})
