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
		containerHandle   string
		fetchResultAction models.FetchResultAction
		logger            *steno.Logger
		err               error
		runOnce           models.RunOnce
		wardenClient      *fake_gordon.FakeGordon
	)

	BeforeEach(func() {
		err = nil
		containerHandle = "some-container-handle"
		fetchResultAction = models.FetchResultAction{
			File: "/tmp/foo",
		}
		logger = steno.NewLogger("test-logger")
		wardenClient = fake_gordon.New()
		runOnce = models.RunOnce{}
	})

	JustBeforeEach(func(done Done) {
		action = New(
			&runOnce,
			fetchResultAction,
			containerHandle,
			"/tmp",
			wardenClient,
			logger,
		)

		result := make(chan error, 1)
		action.Perform(result)
		err = <-result

		close(done)
	})

	Context("when the file exists", func() {
		BeforeEach(func() {
			wardenClient.SetCopyOutFileContent([]byte("result content"))
		})

		It("should return the contents of the file", func() {
			Ω(runOnce.Result).Should(Equal("result content"))
			Ω(err).ShouldNot(HaveOccurred())
		})
	})

	Context("when the file exists but is too large", func() {
		BeforeEach(func() {
			//overflow the (hard-coded) file content limit of 10KB by 1 byte:
			largeFileContent := strings.Repeat("7", 1024*10+1)
			wardenClient.SetCopyOutFileContent([]byte(largeFileContent))
		})

		It("should error", func() {
			Ω(runOnce.Result).Should(BeZero())
			Ω(err).Should(HaveOccurred())
		})
	})

	Context("when the file does not exist", func() {
		BeforeEach(func() {
			wardenClient.SetCopyOutErr(errors.New("kaboom"))
		})

		It("should return an error and an empty result", func() {
			Ω(runOnce.Result).Should(BeZero())
			Ω(err).Should(Equal(errors.New("kaboom")))
		})
	})
})
