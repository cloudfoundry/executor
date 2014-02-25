package actionrunner_test

import (
	"errors"
	"strings"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// additional common variables defined in the actionrunner_suite_test

var _ = Describe("FetchResultRunner", func() {
	var (
		actions []models.ExecutorAction
		result  string
		err     error
	)

	BeforeEach(func() {
		actions = []models.ExecutorAction{
			{
				models.FetchResultAction{
					File: "/tmp/foo",
				},
			},
		}
	})

	JustBeforeEach(func() {
		result, err = runner.Run("handle-x", nil, actions)
	})

	Context("when the file exists", func() {
		BeforeEach(func() {
			gordon.SetCopyOutFileContent([]byte("result content"))
		})

		It("should return the contents of the file", func() {
			Ω(result).Should(Equal("result content"))
			Ω(err).ShouldNot(HaveOccurred())
		})
	})

	Context("when the file exists but is too large", func() {
		BeforeEach(func() {
			//overflow the (hard-coded) file content limit of 10KB by 1 byte:
			largeFileContent := strings.Repeat("7", 1024*10+1)
			gordon.SetCopyOutFileContent([]byte(largeFileContent))
		})

		It("should error", func() {
			Ω(result).Should(BeZero())
			Ω(err).Should(HaveOccurred())
		})
	})

	Context("when the file does not exist", func() {
		BeforeEach(func() {
			gordon.SetCopyOutErr(errors.New("kaboom"))
		})

		It("should return an error and an empty result", func() {
			Ω(result).Should(BeZero())
			Ω(err).Should(Equal(errors.New("kaboom")))
		})
	})
})
