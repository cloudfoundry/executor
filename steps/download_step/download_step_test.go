package download_step_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"strings"

	"github.com/pivotal-golang/cacheddownloader/fakecacheddownloader"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/steps/download_step"
	"github.com/pivotal-golang/archiver/extractor"
	archiveHelper "github.com/pivotal-golang/archiver/extractor/test_helper"
)

type ClosableBuffer struct {
	bytes.Buffer
	closed chan struct{}
}

func NewClosableBuffer() *ClosableBuffer {
	return &ClosableBuffer{closed: make(chan struct{})}
}

func (b *ClosableBuffer) Close() error {
	close(b.closed)
	return nil
}

func (b *ClosableBuffer) IsClosed() bool {
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}

var _ = Describe("DownloadAction", func() {
	var step sequence.Step
	var result chan error

	var downloadAction models.DownloadAction
	var cache *fakecacheddownloader.FakeCachedDownloader
	var tempDir string
	var wardenClient *fake_warden_client.FakeClient
	var logger *steno.Logger

	handle := "some-container-handle"

	BeforeEach(func() {
		var err error

		result = make(chan error)

		cache = &fakecacheddownloader.FakeCachedDownloader{}

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
				CacheKey: "the-cache-key",
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
				extractor.NewZip(),
				tempDir,
				logger,
			)

			stepErr = step.Perform()
		})

		Context("when extract is false", func() {
			var buffer *ClosableBuffer
			var tarReader *tar.Reader

			BeforeEach(func() {
				cache.FetchedContent = []byte(strings.Repeat("7", 1024))

				buffer = NewClosableBuffer()
				tarReader = tar.NewReader(buffer)

				wardenClient.Connection.WhenStreamingIn = func(handle string, dest string) (io.WriteCloser, error) {
					Ω(dest).Should(Equal("/tmp"))
					return buffer, nil
				}
			})

			It("asks the cache for the file", func() {
				Ω(cache.FetchedURL.Host).Should(ContainSubstring("mr_jones"))
				Ω(cache.FetchedCacheKey).Should(Equal("the-cache-key"))
			})

			It("places the file in the container", func() {
				Ω(wardenClient.Connection.StreamedIn(handle)).ShouldNot(BeEmpty())

				header, err := tarReader.Next()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(header.Name).Should(Equal("Antarctica"))
				Ω(header.Mode).Should(Equal(int64(0644)))
				Ω(header.AccessTime.UnixNano()).ShouldNot(BeZero())
				Ω(header.ChangeTime.UnixNano()).ShouldNot(BeZero())

				fileBody, err := ioutil.ReadAll(tarReader)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(fileBody).Should(Equal(cache.FetchedContent))

				Ω(buffer.IsClosed()).Should(BeTrue())
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
					wardenClient.Connection.WhenStreamingIn = func(string, string) (io.WriteCloser, error) {
						return NewClosableBuffer(), expectedErr
					}
				})

				It("returns an error", func() {
					Ω(stepErr).Should(MatchError(expectedErr))
				})
			})
		})

		Context("when extract is true", func() {
			var buffer *ClosableBuffer
			var tarReader *tar.Reader

			BeforeEach(func() {
				downloadAction.Extract = true

				tmpFile, err := ioutil.TempFile("", "some-zip")
				Ω(err).ShouldNot(HaveOccurred())

				archiveHelper.CreateZipArchive(tmpFile.Name(), []archiveHelper.ArchiveFile{
					{
						Name: "file1",
					},
				})

				tmpFile.Seek(0, 0)

				fetchedContent, err := ioutil.ReadAll(tmpFile)
				Ω(err).ShouldNot(HaveOccurred())

				cache.FetchedContent = fetchedContent

				buffer = NewClosableBuffer()
				tarReader = tar.NewReader(buffer)

				wardenClient.Connection.WhenStreamingIn = func(handle string, dest string) (io.WriteCloser, error) {
					Ω(dest).Should(Equal("/tmp/Antarctica"))
					return buffer, nil
				}
			})

			It("does not return an error", func() {
				Ω(stepErr).ShouldNot(HaveOccurred())
			})

			It("places the file in the container under the destination", func() {
				header, err := tarReader.Next()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(header.Name).Should(Equal("./"))

				header, err = tarReader.Next()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(header.Name).Should(Equal("file1"))
			})

			Context("when there is an error extracting the file", func() {
				BeforeEach(func() {
					cache.FetchedContent = []byte("not-a-tgz")
				})

				It("returns an error", func() {
					Ω(stepErr.Error()).Should(ContainSubstring("Extraction failed"))
				})
			})

			Context("when there is an error copying the extracted files into the container", func() {
				var expectedErr = errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingIn = func(string, string) (io.WriteCloser, error) {
						return nil, expectedErr
					}
				})

				It("returns an error", func() {
					Ω(stepErr.Error()).Should(ContainSubstring("Copying into the container failed"))
				})
			})
		})
	})
})
