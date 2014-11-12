package upload_step_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"time"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/depot/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/steps/emittable_error"
	"github.com/cloudfoundry-incubator/executor/depot/steps/upload_step"
	Uploader "github.com/cloudfoundry-incubator/executor/depot/uploader"
	"github.com/cloudfoundry-incubator/executor/depot/uploader/fake_uploader"
	Compressor "github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/lager/lagertest"
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

type fakeUploader struct {
	ready   chan<- struct{}
	barrier <-chan struct{}
}

func (u *fakeUploader) Upload(fileLocation string, destinationUrl *url.URL) (int64, error) {
	u.ready <- struct{}{}
	<-u.barrier
	return 0, nil
}

func newFakeStreamer() *fake_log_streamer.FakeLogStreamer {
	fakeStreamer := new(fake_log_streamer.FakeLogStreamer)

	stdoutBuffer := new(bytes.Buffer)
	stderrBuffer := new(bytes.Buffer)
	fakeStreamer.StdoutReturns(stdoutBuffer)
	fakeStreamer.StderrReturns(stderrBuffer)

	return fakeStreamer
}

var _ = Describe("UploadStep", func() {
	var step sequence.Step
	var result chan error

	var uploadAction *models.UploadAction
	var uploader Uploader.Uploader
	var tempDir string
	var gardenClient *fake_api_client.FakeClient
	var logger *lagertest.TestLogger
	var compressor Compressor.Compressor
	var fakeStreamer *fake_log_streamer.FakeLogStreamer
	var currentUser *user.User
	var uploadTarget *httptest.Server
	var uploadedPayload []byte

	BeforeEach(func() {
		var err error

		result = make(chan error)

		uploadTarget = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var err error

			uploadedPayload, err = ioutil.ReadAll(req.Body)
			Ω(err).ShouldNot(HaveOccurred())

			w.WriteHeader(http.StatusOK)
		}))

		uploadAction = &models.UploadAction{
			To:   uploadTarget.URL,
			From: "./expected-src.txt",
		}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		gardenClient = fake_api_client.New()

		logger = lagertest.NewTestLogger("test")

		compressor = Compressor.NewTgz()
		uploader = Uploader.New(5*time.Second, false, logger)

		fakeStreamer = newFakeStreamer()

		currentUser, err = user.Current()
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
		uploadTarget.Close()
	})

	handle := "some-container-handle"

	JustBeforeEach(func() {
		gardenClient.Connection.CreateReturns(handle, nil)

		container, err := gardenClient.Create(garden_api.ContainerSpec{})
		Ω(err).ShouldNot(HaveOccurred())

		step = upload_step.New(
			container,
			*uploadAction,
			uploader,
			compressor,
			tempDir,
			fakeStreamer,
			make(chan struct{}, 1),
			logger,
		)
	})

	Describe("Perform", func() {
		Context("when streaming out works", func() {
			var buffer *ClosableBuffer

			BeforeEach(func() {
				buffer = NewClosableBuffer()
				gardenClient.Connection.StreamOutStub = func(handle, src string) (io.ReadCloser, error) {
					Ω(src).Should(Equal("./expected-src.txt"))
					Ω(handle).Should(Equal("some-container-handle"))

					tarWriter := tar.NewWriter(buffer)

					dropletContents := "expected-contents"

					err := tarWriter.WriteHeader(&tar.Header{
						Name: "./expected-src.txt",
						Size: int64(len(dropletContents)),
					})
					Ω(err).ShouldNot(HaveOccurred())

					_, err = tarWriter.Write([]byte(dropletContents))
					Ω(err).ShouldNot(HaveOccurred())

					err = tarWriter.Flush()
					Ω(err).ShouldNot(HaveOccurred())

					return buffer, nil
				}
			})

			It("uploads the specified file to the destination", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(uploadedPayload).ShouldNot(BeZero())

				Ω(buffer.IsClosed()).Should(BeTrue())

				Ω(string(uploadedPayload)).Should(Equal("expected-contents"))
			})

			Describe("streaming logs for uploads", func() {
				BeforeEach(func() {
					fakeUploader := new(fake_uploader.FakeUploader)
					fakeUploader.UploadReturns(1024, nil)
					uploader = fakeUploader
				})

				It("streams the upload filesize", func() {
					err := step.Perform()
					Ω(err).ShouldNot(HaveOccurred())

					stdout := fakeStreamer.Stdout().(*bytes.Buffer)
					Ω(stdout.String()).Should(ContainSubstring("Uploaded (1K)"))
				})

				It("does not stream an error", func() {
					err := step.Perform()
					Ω(err).ShouldNot(HaveOccurred())

					stderr := fakeStreamer.Stderr().(*bytes.Buffer)
					Ω(stderr.String()).Should(Equal(""))
				})
			})

			Context("when there is an error uploading", func() {
				errUploadFailed := errors.New("Upload failed!")

				BeforeEach(func() {
					fakeUploader := new(fake_uploader.FakeUploader)
					fakeUploader.UploadReturns(0, errUploadFailed)
					uploader = fakeUploader
				})

				It("returns the appropriate error", func() {
					err := step.Perform()
					Ω(err).Should(MatchError(errUploadFailed))
				})
			})
		})

		Context("when there is an error parsing the upload url", func() {
			BeforeEach(func() {
				uploadAction.To = "foo/bar"
			})

			It("returns the appropriate error", func() {
				err := step.Perform()
				Ω(err).Should(BeAssignableToTypeOf(&url.Error{}))
			})
		})

		Context("when there is an error initiating the stream", func() {
			errStream := errors.New("stream error")

			BeforeEach(func() {
				gardenClient.Connection.StreamOutReturns(nil, errStream)
			})

			It("returns the appropriate error", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(errStream, upload_step.ErrEstablishStream)))
			})
		})

		Context("when there is an error in reading the data from the stream", func() {
			errStream := errors.New("stream error")

			BeforeEach(func() {
				gardenClient.Connection.StreamOutReturns(&errorReader{err: errStream}, nil)
			})

			It("returns the appropriate error", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(errStream, upload_step.ErrReadTar)))
			})
		})
	})

	Describe("the uploads are rate limited", func() {
		var container garden_api.Container

		BeforeEach(func() {
			var err error
			container, err = gardenClient.Create(garden_api.ContainerSpec{
				Handle: handle,
			})
			Ω(err).ShouldNot(HaveOccurred())

			gardenClient.Connection.StreamOutStub = func(handle, src string) (io.ReadCloser, error) {
				buffer := NewClosableBuffer()
				tarWriter := tar.NewWriter(buffer)

				err := tarWriter.WriteHeader(&tar.Header{
					Name: "./does-not-matter.txt",
					Size: int64(0),
				})
				Ω(err).ShouldNot(HaveOccurred())

				return buffer, nil
			}
		})

		It("allows only N concurrent uploads", func() {
			rateLimiter := make(chan struct{}, 2)

			ready := make(chan struct{}, 3)
			barrier := make(chan struct{})
			uploader := &fakeUploader{
				ready:   ready,
				barrier: barrier,
			}

			uploadAction1 := models.UploadAction{
				To:   "http://mybucket.mf",
				From: "./foo1.txt",
			}

			step1 := upload_step.New(
				container,
				uploadAction1,
				uploader,
				compressor,
				tempDir,
				newFakeStreamer(),
				rateLimiter,
				logger,
			)

			uploadAction2 := models.UploadAction{
				To:   "http://mybucket.mf",
				From: "./foo2.txt",
			}

			step2 := upload_step.New(
				container,
				uploadAction2,
				uploader,
				compressor,
				tempDir,
				newFakeStreamer(),
				rateLimiter,
				logger,
			)

			uploadAction3 := models.UploadAction{
				To:   "http://mybucket.mf",
				From: "./foo3.txt",
			}

			step3 := upload_step.New(
				container,
				uploadAction3,
				uploader,
				compressor,
				tempDir,
				newFakeStreamer(),
				rateLimiter,
				logger,
			)

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

			Eventually(ready).Should(Receive())
			Eventually(ready).Should(Receive())
			Consistently(ready).ShouldNot(Receive())

			barrier <- struct{}{}

			Eventually(ready).Should(Receive())

			close(barrier)
		})
	})
})

type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *errorReader) Close() error {
	return nil
}
