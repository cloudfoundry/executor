package runoncehandler_test

import (
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon/fake_gordon"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/downloader/fakedownloader"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/executor/logstreamer"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	. "github.com/cloudfoundry-incubator/executor/runoncehandler"
	"github.com/cloudfoundry-incubator/executor/taskregistry/faketaskregistry"
	"github.com/cloudfoundry-incubator/executor/uploader/fakeuploader"
)

var _ = Describe("RunOnceHandler", func() {
	Describe("RunOnce", func() {
		var (
			handler          *RunOnceHandler
			performedActions []string
			runOnce          models.RunOnce
		)

		spyPerformer := func(actions ...action_runner.Action) error {
			for _, action := range actions {
				actionType := reflect.TypeOf(action).String()
				performedActions = append(performedActions, actionType)
			}

			return nil
		}

		BeforeEach(func() {
			performedActions = []string{}
			runOnce = models.RunOnce{}

			bbs := bbs.BBS{}
			wardenClient := &fake_gordon.FakeGordon{}
			taskRegistry := &faketaskregistry.FakeTaskRegistry{}
			logStreamerFactory := func(models.LogConfig) logstreamer.LogStreamer {
				return nil
			}
			logger := &steno.Logger{}

			backendPlugin := linuxplugin.New()
			downloader := &fakedownloader.FakeDownloader{}
			uploader := &fakeuploader.FakeUploader{}

			runOnceTransformer := run_once_transformer.NewRunOnceTransformer(
				logStreamerFactory,
				downloader,
				uploader,
				backendPlugin,
				wardenClient,
				logger,
				"/fake/temp/dir",
			)

			handler = New(
				bbs,
				wardenClient,
				taskRegistry,
				runOnceTransformer,
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
