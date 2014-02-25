package actionrunner_test

import (
	"errors"
	"os/user"

	"github.com/cloudfoundry-incubator/runtime-schema/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("UploadRunner", func() {
	var (
		actions []models.ExecutorAction
		err     error
	)

	BeforeEach(func() {
		actions = []models.ExecutorAction{
			{
				models.UploadAction{
					To:   "http://mr_jones",
					From: "/Antarctica",
				},
			},
		}
	})

	JustBeforeEach(func() {
		_, err = runner.Run("handle-x", nil, actions)
	})

	It("should upload the file to a URL", func() {
		Ω(uploader.UploadUrls[0].Host).To(ContainSubstring("mr_jones"))
	})

	It("should obtain the file from the container as the current user", func() {
		currentUser, err := user.Current()
		Ω(err).ShouldNot(HaveOccurred())

		copiedFile := gordon.ThingsCopiedOut()[0]
		Ω(copiedFile.Src).To(Equal("/Antarctica"))
		Ω(copiedFile.Owner).Should(Equal(currentUser.Username))
	})

	Context("when obtaining the file from the container goes bad", func() {
		BeforeEach(func() {
			gordon.SetCopyOutErr(errors.New("kaboom"))
		})

		It("should return the error", func() {
			Ω(err).Should(Equal(errors.New("kaboom")))
		})
	})
	Context("when there is an error uploading", func() {
		BeforeEach(func() {
			uploader.AlwaysFail() //and bring shame and dishonor to your house
		})

		It("should return the error", func() {
			Ω(err).ToNot(BeNil())
		})
	})
})
