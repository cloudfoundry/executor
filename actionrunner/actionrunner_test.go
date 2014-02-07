package actionrunner_test

import (
	"errors"
	"time"

	. "github.com/cloudfoundry-incubator/executor/actionrunner"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/gordon/fake_gordon"
	"github.com/vito/gordon/warden"
)

var _ = Describe("ActionRunner", func() {
	var (
		actions     []models.ExecutorAction
		runner      *ActionRunner
		gordon      *fake_gordon.FakeGordon
		linuxPlugin *linuxplugin.LinuxPlugin
	)

	BeforeEach(func() {
		gordon = fake_gordon.New()
		linuxPlugin = linuxplugin.New()
		runner = New(gordon, linuxPlugin)
	})

	Describe("Running the RunAction", func() {
		BeforeEach(func() {
			actions = []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
						Env: map[string]string{
							"A": "1",
						},
					},
				},
			}
		})

		Context("when the script succeeds", func() {
			It("executes the command in the passed-in container", func() {
				err := runner.Run("handle-x", actions)
				Ω(err).ShouldNot(HaveOccurred())

				runningScript := gordon.ScriptsThatRan()[0]
				Ω(runningScript.Handle).Should(Equal("handle-x"))
				Ω(runningScript.Script).Should(Equal("export A=\"1\"\nsudo reboot"))
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

		Context("when the action does not have a timeout", func() {
			It("does not enforce one (i.e. zero-value time.Duration)", func() {
				gordon.WhenRunning("handle-x", "sudo reboot", func() (*warden.RunResponse, error) {
					time.Sleep(100 * time.Millisecond)
					return &warden.RunResponse{}, nil
				})

				err := runner.Run("handle-x", actions)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the action has a timeout", func() {
			BeforeEach(func() {
				actions = []models.ExecutorAction{
					{
						models.RunAction{
							Script:  "sudo reboot",
							Timeout: 100 * time.Millisecond,
						},
					},
				}
			})

			Context("and the script completes in time", func() {
				It("succeeds", func() {
					err := runner.Run("handle-x", actions)
					Ω(err).ShouldNot(HaveOccurred())
				})
			})

			Context("and the script takes longer than the timeout", func() {
				It("returns a RunActionTimeoutError", func() {
					gordon.WhenRunning("handle-x", "sudo reboot", func() (*warden.RunResponse, error) {
						time.Sleep(1 * time.Second)
						return &warden.RunResponse{}, nil
					})

					err := runner.Run("handle-x", actions)
					Ω(err).Should(HaveOccurred())
					Ω(err).Should(Equal(RunActionTimeoutError{models.RunAction{
						Script:  "sudo reboot",
						Timeout: 100 * time.Millisecond,
					}}))
				})
			})
		})
	})
})
