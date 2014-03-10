package runoncehandler

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/lazy_action_runner"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/claim_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/complete_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/create_container_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/execute_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/register_action"
	"github.com/cloudfoundry-incubator/executor/runoncehandler/start_action"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
)

type RunOnceHandlerInterface interface {
	RunOnce(runOnce models.RunOnce, executorId string)
}

type RunOnceHandler struct {
	bbs                Bbs.ExecutorBBS
	wardenClient       gordon.Client
	transformer        *run_once_transformer.RunOnceTransformer
	logStreamerFactory log_streamer_factory.LogStreamerFactory
	logger             *steno.Logger
	taskRegistry       taskregistry.TaskRegistryInterface
}

func New(
	bbs Bbs.ExecutorBBS,
	wardenClient gordon.Client,
	taskRegistry taskregistry.TaskRegistryInterface,
	transformer *run_once_transformer.RunOnceTransformer,
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	logger *steno.Logger,
) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:                bbs,
		wardenClient:       wardenClient,
		taskRegistry:       taskRegistry,
		transformer:        transformer,
		logStreamerFactory: logStreamerFactory,
		logger:             logger,
	}
}

func (handler *RunOnceHandler) RunOnce(runOnce models.RunOnce, executorID string) {
	runner := action_runner.New([]action_runner.Action{
		register_action.New(
			runOnce,
			handler.logger,
			handler.taskRegistry,
		),
		claim_action.New(
			&runOnce,
			handler.logger,
			executorID,
			handler.bbs,
		),
		create_container_action.New(
			&runOnce,
			handler.logger,
			handler.wardenClient,
		),
		start_action.New(
			&runOnce,
			handler.logger,
			handler.bbs,
		),
		execute_action.New(
			&runOnce,
			handler.logger,
			lazy_action_runner.New(func() []action_runner.Action {
				return handler.transformer.ActionsFor(&runOnce)
			}),
		),
		complete_action.New(
			&runOnce,
			handler.logger,
			handler.bbs,
		),
	})

	runner.Perform()
}
