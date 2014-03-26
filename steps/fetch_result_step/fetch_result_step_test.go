package fetch_result_step_test

import (
	"errors"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/sequence"
	. "github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
)

var _ = Describe("FetchResultStep", func() {
	var (
		step              sequence.Step
		fetchResultAction models.FetchResultAction
		logger            *steno.Logger
		wardenClient      *fake_gordon.FakeGordon
		result            string
	)

	BeforeEach(func() {
		result = ""
		fetchResultAction = models.FetchResultAction{
			File: "/tmp/foo",
		}
		logger = steno.NewLogger("test-logger")
		wardenClient = fake_gordon.New()
	})

	JustBeforeEach(func() {
		step = New(
			"handle",
			fetchResultAction,
			"/tmp",
			wardenClient,
			logger,
			&result,
		)
	})

	Context("when the file exists", func() {
		BeforeEach(func() {
			wardenClient.SetCopyOutFileContent([]byte("result content"))
		})

		It("should return the contents of the file", func() {
			err := step.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(result).Should(Equal("result content"))
		})
	})

	Context("when the file exists but is too large", func() {
		BeforeEach(func() {
			//overflow the (hard-coded) file content limit of 10KB by 1 byte:
			largeFileContent := strings.Repeat("7", 1024*10+1)
			wardenClient.SetCopyOutFileContent([]byte(largeFileContent))
		})

		It("should error", func() {
			err := step.Perform()
			Ω(err).Should(HaveOccurred())

			Ω(result).Should(BeZero())
		})
	})

	Context("when the file does not exist", func() {
		disaster := errors.New("kaboom")

		BeforeEach(func() {
			wardenClient.SetCopyOutErr(disaster)
		})

		It("should return an error and an empty result", func() {
			err := step.Perform()
			Ω(err).Should(Equal(disaster))

			Ω(result).Should(BeZero())
		})
	})
})
