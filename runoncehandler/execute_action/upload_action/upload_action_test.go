package upload_action_test

import (
	"errors"
	"io/ioutil"
	"os/user"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/actionrunner/uploader/fakeuploader"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/upload_action"
)

var _ = Describe("UploadAction", func() {
	var action *UploadAction
	var result chan error

	var uploadAction models.UploadAction
	var containerHandle string
	var uploader *fakeuploader.FakeUploader
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

		containerHandle = "some-container-handle"

		uploader = &fakeuploader.FakeUploader{}

		tempDir, err = ioutil.TempDir("", "upload-action-tmpdir")
		Ω(err).ShouldNot(HaveOccurred())

		wardenClient = fake_gordon.New()

		logger = steno.NewLogger("test-logger")
	})

	JustBeforeEach(func() {
		action = New(
			uploadAction,
			containerHandle,
			uploader,
			tempDir,
			wardenClient,
			logger,
		)
	})

	perform := func() {
		result := make(chan error, 1)
		action.Perform(result)
		Ω(<-result).ShouldNot(HaveOccurred())
	}

	Describe("Perform", func() {
		It("uploads the file to the given URL", func() {
			perform()
			Ω(uploader.UploadUrls).ShouldNot(BeEmpty())
			Ω(uploader.UploadUrls[0].Host).To(ContainSubstring("mr_jones"))
		})

		It("places the file in the container", func() {
			perform()

			currentUser, err := user.Current()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.ThingsCopiedOut()).ShouldNot(BeEmpty())

			copiedFile := wardenClient.ThingsCopiedOut()[0]
			Ω(copiedFile.Src).To(Equal("/Antarctica"))
			Ω(copiedFile.Owner).To(Equal(currentUser.Username))
		})

		Context("when there is an error copying the file out", func() {
			disaster := errors.New("no room in the copy inn")

			BeforeEach(func() {
				wardenClient.SetCopyOutErr(disaster)
			})

			It("sends back the error", func() {
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
			})
		})

		Context("when there is an error uploading", func() {
			BeforeEach(func() {
				uploader.AlwaysFail() //and bring shame and dishonor to your house
			})

			It("fails", func() {
				result := make(chan error, 1)
				action.Perform(result)
				Ω(<-result).Should(HaveOccurred())
			})
		})
	})
})
