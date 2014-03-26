package upload_step_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"io/ioutil"
	"os/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	. "github.com/cloudfoundry-incubator/executor/steps/upload_step"
	"github.com/cloudfoundry-incubator/executor/uploader/fake_uploader"
)

var _ = Describe("UploadStep", func() {
	var step sequence.Step
	var result chan error

	var uploadAction models.UploadAction
	var uploader *fake_uploader.FakeUploader
	var tempDir string
	var wardenClient *fake_gordon.FakeGordon
	var logger *steno.Logger

	BeforeEach(func() {
		var err error

		result = make(chan error)

		uploadAction = models.UploadAction{
			To:   "http://mr_jones",
			From: "/Antarctica",
		}

		uploader = &fake_uploader.FakeUploader{}

		tempDir, err = ioutil.TempDir("", "upload-step-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

		logger = steno.NewLogger("test-logger")
	})

	JustBeforeEach(func() {
		step = New(
			"some-container-handle",
			uploadAction,
			uploader,
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
	})
})
