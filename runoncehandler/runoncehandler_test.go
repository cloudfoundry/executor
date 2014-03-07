package runoncehandler_test

import (
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/fakeactionrunner"
	"github.com/cloudfoundry-incubator/executor/actionrunner/logstreamer"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler"
	"github.com/cloudfoundry-incubator/executor/taskregistry/faketaskregistry"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"
)

var _ = Describe("RunOnceHandler", func() {
	Describe("RunOnce", func() {
		var (
			handler          *RunOnceHandler
			performedActions []string
			runOnce          models.RunOnce
		)

		spyPerformer := func(actions ...action_runner.Action) <-chan error {
			for _, action := range actions {
				actionType := reflect.TypeOf(action).String()
				performedActions = append(performedActions, actionType)
			}

			result := make(chan error, 1)
			result <- nil
			return result
		}

		BeforeEach(func() {
			performedActions = []string{}
			runOnce = models.RunOnce{}

			bbs := bbs.BBS{}
			wardenClient := &fake_gordon.FakeGordon{}
			taskRegistry := &faketaskregistry.FakeTaskRegistry{}
			actionRunner := &fakeactionrunner.FakeActionRunner{}
			logStreamerFactory := func(models.LogConfig) logstreamer.LogStreamer {
				return nil
			}
			logger := &steno.Logger{}

			handler = New(
				bbs,
				wardenClient,
				taskRegistry,
				actionRunner,
				spyPerformer,
				logStreamerFactory,
				logger,
			)
		})

		It("performs a sequence of runOnce actions on the specified executor", func() {
			handler.RunOnce(runOnce, "fake-executor-id")

			Ω(performedActions).To(Equal([]string{
				"*register_action.RegisterAction",
				"*claim_action.ClaimAction",
				"*create_container_action.ContainerAction",
				"*start_action.StartAction",
				"*execute_action.ExecuteAction",
				"*complete_action.CompleteAction",
			}))
		})
	})
})
