package claim_action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/fakebbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"

	. "github.com/cloudfoundry-incubator/executor/runoncehandler/claim_action"
)

var _ = Describe("ClaimAction", func() {
	var action *ClaimAction
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
		}

		bbs = fakebbs.NewFakeExecutorBBS()

		action = New(
			&runOnce,
			steno.NewLogger("test-logger"),
			"executor-id",
			bbs,
		)
	})

	Describe("Perform", func() {
		It("claims the RunOnce in the BBS and updates the RunOnce's ExecutorID", func() {
			go action.Perform(result)
			Ω(<-result).Should(BeNil())

			Ω(bbs.ClaimedRunOnce.Guid).Should(Equal(runOnce.Guid))
			Ω(bbs.ClaimedRunOnce.ExecutorID).Should(Equal("executor-id"))
		})

		Context("when registering fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				bbs.ClaimRunOnceErr = disaster
			})

			It("sends back the error", func() {
				go action.Perform(result)
				Ω(<-result).Should(Equal(disaster))
			})
		})
	})
})
