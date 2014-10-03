package upload_step_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"time"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	. "github.com/cloudfoundry-incubator/executor/steps/upload_step"
	Uploader "github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
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
	var stdoutBuffer *bytes.Buffer
	var stderrBuffer *bytes.Buffer

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
			From: "/Antarctica",
		}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		gardenClient = fake_api_client.New()

		logger = lagertest.NewTestLogger("test")

		compressor = Compressor.NewTgz()
		uploader = Uploader.New(5*time.Second, logger)

		fakeStreamer = new(fake_log_streamer.FakeLogStreamer)

		currentUser, err = user.Current()
		Ω(err).ShouldNot(HaveOccurred())

		stdoutBuffer = new(bytes.Buffer)
		stderrBuffer = new(bytes.Buffer)
		fakeStreamer.StdoutReturns(stdoutBuffer)
		fakeStreamer.StderrReturns(stderrBuffer)
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

		step = New(
			container,
			*uploadAction,
			uploader,
			compressor,
			tempDir,
			fakeStreamer,
			logger,
		)
	})

	Describe("Perform", func() {
		Context("when streaming out works", func() {
			var buffer *ClosableBuffer

			BeforeEach(func() {
				buffer = NewClosableBuffer()
				gardenClient.Connection.StreamOutReturns(buffer, nil)
			})

			It("uploads a .tgz to the destination", func() {
				gardenClient.Connection.StreamOutStub = func(handle, src string) (io.ReadCloser, error) {
					Ω(handle).Should(Equal("some-container-handle"))

					if src == "/Antarctica" {
						tarWriter := tar.NewWriter(buffer)

						contents1 := "some-file-contents"

						err := tarWriter.WriteHeader(&tar.Header{
							Name: "some-file",
							Size: int64(len(contents1)),
						})
						Ω(err).ShouldNot(HaveOccurred())

						_, err = tarWriter.Write([]byte(contents1))
						Ω(err).ShouldNot(HaveOccurred())

						err = tarWriter.Flush()
						Ω(err).ShouldNot(HaveOccurred())

						return buffer, nil
					}

					return NewClosableBuffer(), nil
				}

				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(uploadedPayload).ShouldNot(BeZero())

				Ω(buffer.IsClosed()).Should(BeTrue())

				ungzip, err := gzip.NewReader(bytes.NewReader(uploadedPayload))
				Ω(err).ShouldNot(HaveOccurred())

				untar := tar.NewReader(ungzip)

				tarContents := map[string][]byte{}
				for {
					hdr, err := untar.Next()
					if err == io.EOF {
						break
					}

					Ω(err).ShouldNot(HaveOccurred())

					content, err := ioutil.ReadAll(untar)
					Ω(err).ShouldNot(HaveOccurred())

					tarContents[hdr.Name] = content
				}

				Ω(tarContents).Should(HaveKey("some-file"))

				Ω(string(tarContents["some-file"])).Should(Equal("some-file-contents"))
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

					Ω(stdoutBuffer.String()).Should(ContainSubstring("Uploaded (1K)"))
				})

				It("does not stream an error", func() {
					err := step.Perform()
					Ω(err).ShouldNot(HaveOccurred())

					Ω(stderrBuffer.String()).Should(Equal(""))
				})
			})

			Context("when there is an error uploading", func() {
				BeforeEach(func() {
					fakeUploader := new(fake_uploader.FakeUploader)
					fakeUploader.UploadReturns(0, errors.New("Upload failed!"))
					uploader = fakeUploader
				})

				It("returns the error", func() {
					err := step.Perform()
					Ω(err).Should(HaveOccurred())
				})
			})
		})

		Context("when there is an error parsing the upload url", func() {
			BeforeEach(func() {
				uploadAction.To = "foo/bar"
			})

			It("returns the error and loggregates a message to STDERR", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when there is an error initiating the stream", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				gardenClient.Connection.StreamOutReturns(nil, disaster)
			})

			It("returns the error ", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(disaster, "Copying out of the container failed")))
			})
		})

		Context("when there is an error in the middle of streaming the data", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				gardenClient.Connection.StreamOutReturns(&errorReader{err: disaster}, nil)
			})

			It("returns the error ", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(disaster, "Copying out of the container failed")))
			})
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
