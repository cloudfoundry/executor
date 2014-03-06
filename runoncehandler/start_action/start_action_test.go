package start_action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler/start_action"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var _ = Describe("StartAction", func() {
	var action action_runner.Action
	var result chan error

	var runOnce models.RunOnce
	var bbs *fakebbs.FakeExecutorBBS

	BeforeEach(func() {
		result = make(chan error)

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

			ContainerHandle: "some-container-handle",
		}

		bbs = fakebbs.NewFakeExecutorBBS()

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			bbs,
		)
	})

	Describe("Perform", func() {
		It("starts the RunOnce in the BBS", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(bbs.StartedRunOnce.Guid).Should(Equal(runOnce.Guid))
		})

		Context("when starting the RunOnce in the BBS fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.StartRunOnceErr = disaster
			})

			It("sends back the error", func() {
				go action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
			})
		})
	})
})
