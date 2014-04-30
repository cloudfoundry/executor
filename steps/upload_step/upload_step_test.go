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
	"os/user"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	. "github.com/cloudfoundry-incubator/executor/steps/upload_step"
	Uploader "github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
	Compressor "github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/compressor/fake_compressor"
)

var _ = Describe("UploadStep", func() {
	var step sequence.Step
	var result chan error

	var uploadAction *models.UploadAction
	var uploader Uploader.Uploader
	var tempDir string
	var wardenClient *fake_warden_client.FakeClient
	var logger *steno.Logger
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
			From: "/Antarctica",
		}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_warden_client.New()

		logger = steno.NewLogger("test-logger")

		compressor = Compressor.NewTgz()
		uploader = Uploader.New(5*time.Second, logger)

		fakeStreamer = fake_log_streamer.New()

		currentUser, err = user.Current()
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		uploadTarget.Close()
	})

	handle := "some-container-handle"

	JustBeforeEach(func() {
		container, err := wardenClient.Create(warden.ContainerSpec{
			Handle: handle,
		})
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
		It("uploads a .tgz to the destination", func() {
			wardenClient.Connection.WhenCopyingOut = func(handle, src, dst, owner string) error {
				Ω(handle).Should(Equal("some-container-handle"))
				Ω(owner).Should(Equal(currentUser.Username))

				if src == "/Antarctica" {
					err := ioutil.WriteFile(
						filepath.Join(dst, "some-file"),
						[]byte("some-file-contents"),
						0644,
					)
					Ω(err).ShouldNot(HaveOccurred())

					err = ioutil.WriteFile(
						filepath.Join(dst, "another-file"),
						[]byte("another-file-contents"),
						0644,
					)
					Ω(err).ShouldNot(HaveOccurred())
				}

				return nil
			}

			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(uploadedPayload).ShouldNot(BeZero())

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
			Ω(tarContents).Should(HaveKey("another-file"))
			Ω(string(tarContents["some-file"])).Should(Equal("some-file-contents"))
			Ω(string(tarContents["another-file"])).Should(Equal("another-file-contents"))
		})

		Describe("streaming logs for uploads", func() {
			BeforeEach(func() {
				fakeUploader := &fake_uploader.FakeUploader{}
				fakeUploader.UploadSize = 1024

				uploader = fakeUploader
			})

			It("streams the upload filesize", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StdoutBuffer.String()).Should(ContainSubstring("Uploaded (1K)"))
			})

			It("does not stream an error", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StderrBuffer.String()).Should(Equal(""))
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

		Context("when there is an error copying the file out", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				wardenClient.Connection.WhenCopyingOut = func(handle, src, dst, owner string) error {
					return disaster
				}
			})

			It("returns the error ", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(disaster, "Copying out of the container failed")))
			})
		})

		Context("and compressing fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeCompressor := &fake_compressor.FakeCompressor{}
				fakeCompressor.CompressError = disaster

				compressor = fakeCompressor
			})

			It("returns the error and loggregates a message to STDERR stream", func() {
				err := step.Perform()
				Ω(err).Should(MatchError(emittable_error.New(disaster, "Compression failed")))
			})
		})

		Context("when there is an error uploading", func() {
			BeforeEach(func() {
				fakeUploader := &fake_uploader.FakeUploader{}
				fakeUploader.AlwaysFail() //and bring shame and dishonor to your house

				uploader = fakeUploader
			})

			It("returns the error", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
