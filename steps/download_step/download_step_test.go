package download_step_test

import (
	"errors"
	"io/ioutil"
	"strings"

	"github.com/cloudfoundry-incubator/executor/file_cache/fake_file_cache"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"
)

var _ = Describe("DownloadAction", func() {
	var step sequence.Step
	var result chan error

	var downloadAction models.DownloadAction
	var cache *fake_file_cache.FakeFileCache
	var extractor *fake_extractor.FakeExtractor
	var tempDir string
	var wardenClient *fake_warden_client.FakeClient
	var logger *steno.Logger

	handle := "some-container-handle"

	BeforeEach(func() {
		var err error

		result = make(chan error)

		cache = &fake_file_cache.FakeFileCache{}
		cache.FetchedContent = []byte(strings.Repeat("7", 1024))
		extractor = &fake_extractor.FakeExtractor{}

		tempDir, err = ioutil.TempDir("", "download-action-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_warden_client.New()

		logger = steno.NewLogger("test-logger")
	})

	Describe("Perform", func() {
		var stepErr error

		BeforeEach(func() {
			downloadAction = models.DownloadAction{
				From:     "http://mr_jones",
				To:       "/tmp/Antarctica",
				Extract:  false,
				CacheKey: "foo",
			}
		})

		JustBeforeEach(func() {
			container, err := wardenClient.Create(warden.ContainerSpec{
				Handle: handle,
			})
			Ω(err).ShouldNot(HaveOccurred())

			step = New(
				container,
				downloadAction,
				cache,
				extractor,
				tempDir,
				logger,
			)

			stepErr = step.Perform()
		})

		It("asks the cache for the file", func() {
			Ω(cache.FetchedURL.Host).Should(ContainSubstring("mr_jones"))
			Ω(cache.FetchedCacheKey).Should(Equal("foo"))
		})

		It("places the file in the container", func() {
			copiedIn := wardenClient.Connection.CopiedIn(handle)
			Ω(copiedIn).ShouldNot(BeEmpty())

			Ω(copiedIn[0].Source).To(ContainSubstring(tempDir))
			Ω(copiedIn[0].Destination).To(Equal("/tmp/Antarctica"))
		})

		It("does not return an error", func() {
			Ω(stepErr).ShouldNot(HaveOccurred())
		})

		Context("when there is an error parsing the download url", func() {
			BeforeEach(func() {
				downloadAction.From = "foo/bar"
			})

			It("returns an error", func() {
				Ω(stepErr).Should(HaveOccurred())
			})
		})

		Context("when there is an error fetching the file", func() {
			BeforeEach(func() {
				cache.FetchError = errors.New("bam")
			})

			It("returns an error", func() {
				Ω(stepErr).Should(MatchError(cache.FetchError))
			})
		})

		Context("when there is an error copying the file into the container", func() {
			var expectedErr = errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenCopyingIn = func(string, string, string) error {
					return expectedErr
				}
			})

			It("returns an error", func() {
				Ω(stepErr).Should(MatchError(emittable_error.New(expectedErr, "Copying into the container failed")))
			})
		})

		Context("when extract is true", func() {
			BeforeEach(func() {
				downloadAction.Extract = true
			})

			It("does not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("uses the specified extractor", func() {
				src, dest := extractor.ExtractInput()
				Ω(src).To(ContainSubstring(tempDir))
				Ω(dest).To(ContainSubstring(tempDir))
			})

			It("places the file in the container under the destination", func() {
				copiedIn := wardenClient.Connection.CopiedIn(handle)
				Ω(copiedIn).ShouldNot(BeEmpty())

				Ω(copiedIn[0].Source).To(ContainSubstring(tempDir))
				Ω(copiedIn[0].Destination).To(Equal("/tmp/Antarctica/"))
			})

			Context("when there is an error extracting the file", func() {
				var expectedErr = errors.New("extraction failed")
				BeforeEach(func() {
					extractor.SetExtractOutput(expectedErr)
				})

				It("returns an error", func() {
					Ω(stepErr).Should(MatchError(emittable_error.New(expectedErr, "Extraction failed")))
				})
			})

			Context("when there is an error copying the extracted files into the container", func() {
				var expectedErr = errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenCopyingIn = func(string, string, string) error {
						return expectedErr
					}
				})

				It("returns an error", func() {
					Ω(stepErr).Should(MatchError(emittable_error.New(expectedErr, "Copying into the container failed")))
				})
			})
		})
	})
})
