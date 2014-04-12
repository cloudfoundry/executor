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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer/fake_log_streamer"
	"github.com/cloudfoundry-incubator/executor/sequence"
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
	var wardenClient *fake_gordon.FakeGordon
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
			Name: "Mr. Jones",
			To:   uploadTarget.URL,
			From: "/Antarctica",
		}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

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

	JustBeforeEach(func() {
		step = New(
			"some-container-handle",
			*uploadAction,
			uploader,
			compressor,
			tempDir,
			wardenClient,
			fakeStreamer,
			logger,
		)
	})

	Describe("Perform", func() {
		It("uploads a .tgz to the destination", func() {
			wardenClient.WhenCopyingOut(fake_gordon.CopiedOut{
				Handle: "some-container-handle",
				Src:    "/Antarctica",
			}, func(out fake_gordon.CopiedOut) error {
				err := ioutil.WriteFile(
					filepath.Join(out.Dst, "some-file"),
					[]byte("some-file-contents"),
					0644,
				)
				Ω(err).ShouldNot(HaveOccurred())

				err = ioutil.WriteFile(
					filepath.Join(out.Dst, "another-file"),
					[]byte("another-file-contents"),
					0644,
				)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(out.Owner).To(Equal(currentUser.Username))

				return nil
			})

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

			It("streams an upload message", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StdoutBuffer.String()).Should(ContainSubstring("Uploading Mr. Jones\n"))
			})

			It("streams the upload filesize", func() {
				err := step.Perform()
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeStreamer.StdoutBuffer.String()).Should(ContainSubstring("(1K)"))
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

				Ω(fakeStreamer.StderrBuffer.String()).Should(ContainSubstring("Uploading Mr. Jones failed\n"))
			})
		})

		Context("when there is an error copying the file out", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				wardenClient.WhenCopyingOut(fake_gordon.CopiedOut{
					Handle: "some-container-handle",
					Src:    "/Antarctica",
				}, func(fake_gordon.CopiedOut) error {
					return disaster
				})
			})

			It("returns the error loggregates a message to STDERR stream", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())

				Ω(fakeStreamer.StderrBuffer.String()).Should(ContainSubstring("Uploading Mr. Jones failed\n"))
			})
		})

		Context("when there is an error uploading", func() {
			BeforeEach(func() {
				fakeUploader := &fake_uploader.FakeUploader{}
				fakeUploader.AlwaysFail() //and bring shame and dishonor to your house

				uploader = fakeUploader
			})

			It("returns the error and loggregates a message to STDERR stream", func() {
				err := step.Perform()
				Ω(err).Should(HaveOccurred())

				Ω(fakeStreamer.StderrBuffer.String()).Should(ContainSubstring("Uploading Mr. Jones failed\n"))
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
				Ω(err).Should(Equal(disaster))

				Ω(fakeStreamer.StderrBuffer.String()).Should(ContainSubstring("Uploading Mr. Jones failed\n"))
			})
		})
	})
})
