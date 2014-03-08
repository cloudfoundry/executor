package fetch_result_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action/fetch_result_action"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
	"strings"
)

var _ = Describe("FetchResultAction", func() {
	var (
		action            action_runner.Action
		fetchResultAction models.FetchResultAction
		logger            *steno.Logger
		runOnce           *models.RunOnce
		wardenClient      *fake_gordon.FakeGordon
	)

	BeforeEach(func() {
		runOnce = &models.RunOnce{}
		fetchResultAction = models.FetchResultAction{
			File: "/tmp/foo",
		}
		logger = steno.NewLogger("test-logger")
		wardenClient = fake_gordon.New()
	})

	JustBeforeEach(func() {
		action = New(
			runOnce,
			fetchResultAction,
			"/tmp",
			wardenClient,
			logger,
		)
	})

	Context("when the file exists", func() {
		BeforeEach(func() {
			wardenClient.SetCopyOutFileContent([]byte("result content"))
		})

		It("should return the contents of the file", func() {
			err := action.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(runOnce.Result).Should(Equal("result content"))
		})
	})

	Context("when the file exists but is too large", func() {
		BeforeEach(func() {
			//overflow the (hard-coded) file content limit of 10KB by 1 byte:
			largeFileContent := strings.Repeat("7", 1024*10+1)
			wardenClient.SetCopyOutFileContent([]byte(largeFileContent))
		})

		It("should error", func() {
			err := action.Perform()
			Ω(err).Should(HaveOccurred())

			Ω(runOnce.Result).Should(BeZero())
		})
	})

	Context("when the file does not exist", func() {
		disaster := errors.New("kaboom")

		BeforeEach(func() {
			wardenClient.SetCopyOutErr(disaster)
		})

		It("should return an error and an empty result", func() {
			err := action.Perform()
			Ω(err).Should(Equal(disaster))

			Ω(runOnce.Result).Should(BeZero())
		})
	})
})
