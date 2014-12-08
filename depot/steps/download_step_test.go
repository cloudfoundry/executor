package steps_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"

	"github.com/pivotal-golang/cacheddownloader"
	cdfakes "github.com/pivotal-golang/cacheddownloader/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer/fake_log_streamer"
	. "github.com/cloudfoundry-incubator/executor/depot/steps"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	archiveHelper "github.com/pivotal-golang/archiver/extractor/test_helper"
)

var _ = Describe("DownloadAction", func() {
	var step Step

	var downloadAction models.DownloadAction
	var cache *cdfakes.FakeCachedDownloader
	var gardenClient *fake_api_client.FakeClient
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var logger *lagertest.TestLogger
	var rateLimiter chan struct{}

	handle := "some-container-handle"

	BeforeEach(func() {
		cache = &cdfakes.FakeCachedDownloader{}
		cache.FetchReturns(ioutil.NopCloser(new(bytes.Buffer)), nil)

		downloadAction = models.DownloadAction{
			From:     "http://mr_jones",
			To:       "/tmp/Antarctica",
			CacheKey: "the-cache-key",
		}

		gardenClient = fake_api_client.New()

		fakeStreamer = newFakeStreamer()
		logger = lagertest.NewTestLogger("test")

		rateLimiter = make(chan struct{}, 1)
	})

	Describe("Perform", func() {
		var stepErr error

		JustBeforeEach(func() {
			container, err := gardenClient.Create(garden_api.ContainerSpec{
				Handle: handle,
			})
			Ω(err).ShouldNot(HaveOccurred())

			step = NewDownload(
				container,
				downloadAction,
				cache,
				rateLimiter,
				fakeStreamer,
				logger,
			)

			stepErr = step.Perform()
		})

		var tarReader *tar.Reader

		It("downloads via the cache with a tar transformer", func() {
			Ω(cache.FetchCallCount()).Should(Equal(1))

			url, cacheKey, transformer, cancelChan := cache.FetchArgsForCall(0)
			Ω(url.Host).Should(ContainSubstring("mr_jones"))
			Ω(cacheKey).Should(Equal("the-cache-key"))
			Ω(cancelChan).ShouldNot(BeNil())

			tVal := reflect.ValueOf(transformer)
			expectedVal := reflect.ValueOf(cacheddownloader.TarTransform)

			Ω(tVal.Pointer()).Should(Equal(expectedVal.Pointer()))
		})

		It("logs the step", func() {
			Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
				"test.download-step.starting-download",
				"test.download-step.finished-download",
			}))
		})

		Context("when an artifact is not specified", func() {
			It("does not stream the download information", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				stdout := fakeStreamer.Stdout().(*bytes.Buffer)
				Ω(stdout.String()).Should(BeEmpty())
			})
		})

		Context("when an artifact is specified", func() {
			BeforeEach(func() {
				downloadAction.Artifact = "artifact"
			})

			Context("streams the downloaded filesize", func() {
				It("streams unknown when the Fetch does not return a File", func() {
					Ω(stepErr).ShouldNot(HaveOccurred())

					stdout := fakeStreamer.Stdout().(*bytes.Buffer)
					Ω(stdout.String()).Should(ContainSubstring("Downloaded artifact (unknown)"))
				})

				Context("with a file", func() {
					var tempFile *os.File

					BeforeEach(func() {
						var err error
						tempFile, err = ioutil.TempFile("", "download-step")
						Ω(err).ShouldNot(HaveOccurred())
						ioutil.WriteFile(tempFile.Name(), []byte("data"), os.ModePerm)
						cache.FetchReturns(cacheddownloader.NewFileCloser(tempFile, func(string) {}), nil)
					})

					AfterEach(func() {
						os.Remove(tempFile.Name())
					})

					It("streams the size when the Fetch returns a File", func() {
						Ω(stepErr).ShouldNot(HaveOccurred())

						stdout := fakeStreamer.Stdout().(*bytes.Buffer)
						Ω(stdout.String()).Should(ContainSubstring("Downloaded artifact (4)"))
					})
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

			It("logs the step", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.download-step.starting-download",
					"test.download-step.parse-request-uri-error",
				}))
			})
		})

		Context("and the fetched bits are a valid tarball", func() {
			BeforeEach(func() {
				tarFile := createTempTar()
				defer os.Remove(tarFile.Name())

				cache.FetchReturns(tarFile, nil)
			})

			Context("and streaming in succeeds", func() {
				BeforeEach(func() {
					buffer := &bytes.Buffer{}
					tarReader = tar.NewReader(buffer)

					gardenClient.Connection.StreamInStub = func(handle string, dest string, tarStream io.Reader) error {
						Ω(dest).Should(Equal("/tmp/Antarctica"))

						_, err := io.Copy(buffer, tarStream)
						Ω(err).ShouldNot(HaveOccurred())

						return nil
					}
				})

				It("does not return an error", func() {
					Ω(stepErr).ShouldNot(HaveOccurred())
				})

				It("places the file in the container under the destination", func() {
					header, err := tarReader.Next()
					Ω(err).ShouldNot(HaveOccurred())
					Ω(header.Name).Should(Equal("file1"))
				})
			})

			Context("when there is an error copying the extracted files into the container", func() {
				var expectedErr = errors.New("oh no!")

				BeforeEach(func() {
					gardenClient.Connection.StreamInReturns(expectedErr)
				})

				It("returns an error", func() {
					Ω(stepErr.Error()).Should(ContainSubstring("Copying into the container failed"))
				})

				It("logs the step", func() {
					Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
						"test.download-step.starting-download",
						"test.download-step.finished-download",
						"test.download-step.failed-to-stream-in",
					}))
				})
			})
		})

		Context("when there is an error fetching the file", func() {
			BeforeEach(func() {
				cache.FetchReturns(nil, errors.New("oh no!"))
			})

			It("returns an error", func() {
				Ω(stepErr.Error()).Should(ContainSubstring("Downloading failed"))
			})

			It("logs the step", func() {
				Ω(logger.TestSink.LogMessages()).Should(ConsistOf([]string{
					"test.download-step.starting-download",
					"test.download-step.failed-to-fetch",
				}))
			})
		})

	})

	Describe("Cancel", func() {
		var result chan error

		BeforeEach(func() {
			result = make(chan error)

			container, err := gardenClient.Create(garden_api.ContainerSpec{
				Handle: handle,
			})
			Ω(err).ShouldNot(HaveOccurred())

			step = NewDownload(
				container,
				downloadAction,
				cache,
				rateLimiter,
				fakeStreamer,
				logger,
			)
		})

		Context("when waiting on the rate limiter", func() {
			JustBeforeEach(func() {
				rateLimiter <- struct{}{}
				go func() { result <- step.Perform() }()
			})

			It("cancels the wait", func() {
				step.Cancel()
				Eventually(result).Should(Receive(MatchError(CancelError{})))
			})

			It("does not fetch the download artifact", func() {
				step.Cancel()
				Eventually(result).Should(Receive(MatchError(CancelError{})))
				Ω(cache.FetchCallCount()).Should(Equal(0))
			})
		})

		Context("when downloading the file", func() {
			var calledChan chan struct{}

			BeforeEach(func() {
				calledChan = make(chan struct{})

				cache.FetchStub = func(u *url.URL, key string, t cacheddownloader.CacheTransformer, cancelCh <-chan struct{}) (io.ReadCloser, error) {
					Ω(cancelCh).ShouldNot(BeNil())
					Ω(cancelCh).ShouldNot(BeClosed())

					close(calledChan)
					<-cancelCh

					Ω(cancelCh).Should(BeClosed())

					return nil, errors.New("some error indicating a cancel")
				}
			})

			JustBeforeEach(func() {
				go func() { result <- step.Perform() }()
			})

			It("closes the cancel channel and propagates the cancel error", func() {
				Eventually(calledChan).Should(BeClosed())
				step.Cancel()

				Eventually(result).Should(Receive(MatchError(CancelError{})))
			})
		})

		Context("when streaming the file into the container", func() {
			var calledChan chan struct{}
			var barrierChan chan struct{}

			BeforeEach(func() {
				tarFile := createTempTar()
				defer os.Remove(tarFile.Name())
				cache.FetchReturns(tarFile, nil)

				calledChan = make(chan struct{})
				barrierChan = make(chan struct{})

				gardenClient.Connection.StreamInStub = func(handle string, dest string, tarStream io.Reader) error {
					writer := func(p []byte) (n int, err error) {
						close(calledChan)
						<-barrierChan
						return 1, nil
					}
					_, err := io.Copy(WriteFunc(writer), tarStream)
					return err
				}
			})

			JustBeforeEach(func() {
				go func() { result <- step.Perform() }()
			})

			It("aborts the streaming", func() {
				Eventually(calledChan).Should(BeClosed())
				step.Cancel()
				close(barrierChan)

				Eventually(result).Should(Receive(MatchError(CancelError{})))
			})
		})
	})

	Describe("the downloads are rate limited", func() {
		var container garden_api.Container

		BeforeEach(func() {
			var err error
			container, err = gardenClient.Create(garden_api.ContainerSpec{
				Handle: handle,
			})
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("allows only N concurrent downloads", func() {
			rateLimiter := make(chan struct{}, 2)

			downloadAction1 := models.DownloadAction{
				From: "http://mr_jones1",
				To:   "/tmp/Antarctica",
			}

			step1 := NewDownload(
				container,
				downloadAction1,
				cache,
				rateLimiter,
				fakeStreamer,
				logger,
			)

			downloadAction2 := models.DownloadAction{
				From: "http://mr_jones2",
				To:   "/tmp/Antarctica",
			}

			step2 := NewDownload(
				container,
				downloadAction2,
				cache,
				rateLimiter,
				fakeStreamer,
				logger,
			)

			downloadAction3 := models.DownloadAction{
				From: "http://mr_jones3",
				To:   "/tmp/Antarctica",
			}

			step3 := NewDownload(
				container,
				downloadAction3,
				cache,
				rateLimiter,
				fakeStreamer,
				logger,
			)

			fetchCh := make(chan struct{}, 3)
			barrier := make(chan struct{})
			nopCloser := ioutil.NopCloser(new(bytes.Buffer))
			cache.FetchStub = func(urlToFetch *url.URL, cacheKey string, transformer cacheddownloader.CacheTransformer, cancelChan <-chan struct{}) (io.ReadCloser, error) {
				fetchCh <- struct{}{}
				<-barrier
				return nopCloser, nil
			}

			go func() {
				defer GinkgoRecover()

				err := step1.Perform()
				Ω(err).ShouldNot(HaveOccurred())
			}()
			go func() {
				defer GinkgoRecover()

				err := step2.Perform()
				Ω(err).ShouldNot(HaveOccurred())
			}()
			go func() {
				defer GinkgoRecover()

				err := step3.Perform()
				Ω(err).ShouldNot(HaveOccurred())
			}()

			Eventually(fetchCh).Should(Receive())
			Eventually(fetchCh).Should(Receive())
			Consistently(fetchCh).ShouldNot(Receive())

			barrier <- struct{}{}

			Eventually(fetchCh).Should(Receive())

			close(barrier)
		})
	})
})

func createTempTar() *os.File {
	tarFile, err := ioutil.TempFile("", "some-tar")
	Ω(err).ShouldNot(HaveOccurred())

	archiveHelper.CreateTarArchive(
		tarFile.Name(),
		[]archiveHelper.ArchiveFile{{Name: "file1"}},
	)

	_, err = tarFile.Seek(0, 0)
	Ω(err).ShouldNot(HaveOccurred())

	return tarFile
}

type WriteFunc func(p []byte) (n int, err error)

func (wf WriteFunc) Write(p []byte) (n int, err error) {
	return wf(p)
}
