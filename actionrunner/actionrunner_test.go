package actionrunner_test

import (
	"errors"
	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("ActionRunner", func() {
	var (
		actions []models.ExecutorAction
		runner  *ActionRunner
		gordon  *fake_gordon.FakeGordon
	)

	BeforeEach(func() {
		gordon = fake_gordon.New()
		runner = New(gordon)
	})

	Describe("Running the RunAction", func() {
		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.RunAction{"sudo reboot"},
				},
			}
		})

		Context("when the script succeeds", func() {
			It("executes the command in the passed-in container", func() {
				err := runner.Run("handle-x", actions)
				Ω(err).ShouldNot(HaveOccurred())

				runningScript := gordon.ScriptsThatRan()[0]
				Ω(runningScript.Handle).Should(Equal("handle-x"))
				Ω(runningScript.Script).Should(Equal("sudo reboot"))
			})
		})

		Context("when gordon errors", func() {
			BeforeEach(func() {
				gordon.SetRunReturnValues(0, errors.New("I, like, tried but failed"))
			})

			It("should return the error", func() {
				err := runner.Run("handle-x", actions)
				Ω(err).Should(Equal(errors.New("I, like, tried but failed")))
			})
		})

		Context("when the script has a non-zero exit code", func() {
			BeforeEach(func() {
				gordon.SetRunReturnValues(19, nil)
			})

			It("should return an error with the exit code", func() {
				err := runner.Run("handle-x", actions)
				Ω(err.Error()).Should(ContainSubstring("19"))
			})
		})
	})
})
