package start_action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	. "github.com/cloudfoundry-incubator/executor/run_once_handler/start_action"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("StartAction", func() {
	var action action_runner.Action

	var runOnce models.RunOnce
	var bbs *fake_bbs.FakeExecutorBBS
	var containerHandle string

	BeforeEach(func() {
		runOnce = models.RunOnce{
			Guid:  "totally-unique",
			Stack: "penguin",
			Actions: []models.ExecutorAction{
				{
					models.RunAction{
						Script: "sudo reboot",
					},
				},
			},

			ExecutorID: "some-executor-id",
		}

		bbs = fake_bbs.NewFakeExecutorBBS()
		containerHandle = "some-container-handle"

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			bbs,
			&containerHandle,
		)
	})

	Describe("Perform", func() {
		It("starts the RunOnce in the BBS", func() {
			err := action.Perform()
			Ω(err).ShouldNot(HaveOccurred())

			started := bbs.StartedRunOnces()
			Ω(started).ShouldNot(BeEmpty())
			Ω(started[0].Guid).Should(Equal(runOnce.Guid))
			Ω(started[0].ContainerHandle).Should(Equal(containerHandle))
		})

		Context("when starting the RunOnce in the BBS fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.SetStartRunOnceErr(disaster)
			})

			It("sends back the error", func() {
				err := action.Perform()
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
