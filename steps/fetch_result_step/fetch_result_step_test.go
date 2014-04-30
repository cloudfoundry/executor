package fetch_result_step_test

import (
	"errors"
	"io/ioutil"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/steps/emittable_error"
	. "github.com/cloudfoundry-incubator/executor/steps/fetch_result_step"
)

var _ = Describe("FetchResultStep", func() {
	var (
		step              sequence.Step
		fetchResultAction models.FetchResultAction
		logger            *steno.Logger
		wardenClient      *fake_warden_client.FakeClient
		result            string
	)

	handle := "some-container-handle"

	BeforeEach(func() {
		result = ""

		fetchResultAction = models.FetchResultAction{
			File: "/tmp/foo",
		}

		logger = steno.NewLogger("test-logger")

		wardenClient = fake_warden_client.New()
	})

	JustBeforeEach(func() {
		container, err := wardenClient.Create(warden.ContainerSpec{
			Handle: handle,
		})
		Ω(err).ShouldNot(HaveOccurred())

		step = New(
			container,
			fetchResultAction,
			"/tmp",
			logger,
			&result,
		)
	})

	Context("when the file exists", func() {
		BeforeEach(func() {
			wardenClient.Connection.WhenCopyingOut = func(handle, src, dst, owner string) error {
				err := ioutil.WriteFile(dst, []byte("result content"), 0644)
				Ω(err).ShouldNot(HaveOccurred())

				return nil
			}
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

			wardenClient.Connection.WhenCopyingOut = func(handle, src, dst, owner string) error {
				err := ioutil.WriteFile(dst, []byte(largeFileContent), 0644)
				Ω(err).ShouldNot(HaveOccurred())

				return nil
			}
		})

		It("should error", func() {
			err := step.Perform()
			Ω(err.Error()).Should(ContainSubstring("Copying out of the container failed"))
			Ω(err.Error()).Should(ContainSubstring("result file size exceeds allowed limit"))

			Ω(result).Should(BeZero())
		})
	})

	Context("when the file does not exist", func() {
		disaster := errors.New("kaboom")

		BeforeEach(func() {
			wardenClient.Connection.WhenCopyingOut = func(handle, src, dst, owner string) error {
				return disaster
			}
		})

		It("should return an error and an empty result", func() {
			err := step.Perform()
			Ω(err).Should(MatchError(emittable_error.New(disaster, "Copying out of the container failed")))

			Ω(result).Should(BeZero())
		})
	})
})
