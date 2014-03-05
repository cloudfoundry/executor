package complete_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/runoncehandler/complete_action"
)

var _ = Describe("CompleteAction", func() {
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

			// Result and Failed being present at the same time is
			// inaccurate but allows us to test that we save both,
			// otherwise Failed is just false which is a zero value
			Result:        "some-result-payload",
			Failed:        true,
			FailureReason: "because i said so",
		}

		bbs = fakebbs.NewFakeExecutorBBS()

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			bbs,
		)
	})

	Describe("Perform", func() {
		It("completes the RunOnce in the BBS and updates the RunOnce's Failed/FailureReason/Result", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(bbs.CompletedRunOnce.Guid).Should(Equal(runOnce.Guid))
			Ω(bbs.CompletedRunOnce.Result).Should(Equal(runOnce.Result))
			Ω(bbs.CompletedRunOnce.Failed).Should(Equal(runOnce.Failed))
			Ω(bbs.CompletedRunOnce.FailureReason).Should(Equal(runOnce.FailureReason))
		})

		Context("when completing fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.CompleteRunOnceErr = disaster
			})

			It("sends back the error", func() {
				go action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
			})
		})
	})
})
