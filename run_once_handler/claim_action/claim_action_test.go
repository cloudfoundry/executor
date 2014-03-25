package claim_action_test

import (
	"errors"
	"github.com/cloudfoundry-incubator/executor/action_runner"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/run_once_handler/claim_action"
)

var _ = Describe("ClaimAction", func() {
	var action action_runner.Action

	var runOnce models.RunOnce
	var bbs *fake_bbs.FakeExecutorBBS

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
		}

		bbs = fake_bbs.NewFakeExecutorBBS()

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			"executor-id",
			bbs,
		)
	})

	Describe("Perform", func() {
		It("claims the RunOnce in the BBS and updates the RunOnce's ExecutorID", func() {
			Ω(action.Perform()).Should(BeNil())

			claimed := bbs.ClaimedRunOnces()
			Ω(claimed).ShouldNot(BeEmpty())
			Ω(claimed[0].Guid).Should(Equal(runOnce.Guid))
			Ω(claimed[0].ExecutorID).Should(Equal("executor-id"))
		})

		Context("when registering fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.SetClaimRunOnceErr(disaster)
			})

			It("returns the error", func() {
				Ω(action.Perform()).Should(Equal(disaster))
			})
		})
	})
})
