package action_runner_test

import (
	. "github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/fake_action"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Run", func() {
	var (
		actions          []Action
		performedActions []string
	)

	spyAction := func(name string) fake_action.FakeAction {
		return fake_action.FakeAction{
			WhenPerforming: func() error {
				performedActions = append(performedActions, name)
				return nil
			},
		}
	}

	BeforeEach(func() {
		performedActions = []string{}

		actions = []Action{
			spyAction("foo"),
			spyAction("bar"),
		}
	})

	It("runs the provided actions", func() {
		err := Run(actions...)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(performedActions).To(Equal([]string{"foo", "bar"}))
	})
})
