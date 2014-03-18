package run_once_handler

import (
	"github.com/cloudfoundry-incubator/executor/run_once_handler/limit_container_action"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/action_runner"
	"github.com/cloudfoundry-incubator/executor/action_runner/lazy_action_runner"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/claim_action"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/create_container_action"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/execute_action"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/register_action"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/start_action"
	"github.com/cloudfoundry-incubator/executor/task_registry"
)

type RunOnceHandlerInterface interface {
	RunOnce(runOnce *models.RunOnce, executorId string, cancel <-chan struct{})
}

type RunOnceHandler struct {
	bbs                 Bbs.ExecutorBBS
	wardenClient        gordon.Client
	transformer         *run_once_transformer.RunOnceTransformer
	logStreamerFactory  log_streamer_factory.LogStreamerFactory
	logger              *steno.Logger
	taskRegistry        task_registry.TaskRegistryInterface
	containerInodeLimit int
}

func New(
	bbs Bbs.ExecutorBBS,
	wardenClient gordon.Client,
	taskRegistry task_registry.TaskRegistryInterface,
	transformer *run_once_transformer.RunOnceTransformer,
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	logger *steno.Logger,
	containerInodeLimit int,
) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:                 bbs,
		wardenClient:        wardenClient,
		taskRegistry:        taskRegistry,
		transformer:         transformer,
		logStreamerFactory:  logStreamerFactory,
		logger:              logger,
		containerInodeLimit: containerInodeLimit,
	}
}

func (handler *RunOnceHandler) RunOnce(runOnce *models.RunOnce, executorID string, cancel <-chan struct{}) {
	var containerHandle string
	var runOnceResult string
	runner := action_runner.New([]action_runner.Action{
		register_action.New(
			runOnce,
			handler.logger,
			handler.taskRegistry,
		),
		claim_action.New(
			runOnce,
			handler.logger,
			executorID,
			handler.bbs,
		),
		create_container_action.New(
			runOnce,
			handler.logger,
			handler.wardenClient,
			&containerHandle,
		),
		limit_container_action.New(
			runOnce,
			handler.logger,
			handler.wardenClient,
			handler.containerInodeLimit,
			&containerHandle,
		),
		start_action.New(
			runOnce,
			handler.logger,
			handler.bbs,
			&containerHandle,
		),
		execute_action.New(
			runOnce,
			handler.logger,
			lazy_action_runner.New(func() []action_runner.Action {
				return handler.transformer.ActionsFor(runOnce, &containerHandle, &runOnceResult)
			}),
			handler.bbs,
			&runOnceResult,
		),
	})

	result := make(chan error, 1)

	go func() {
		result <- runner.Perform()
	}()

	for {
		select {
		case <-result:
			return
		case <-cancel:
			runner.Cancel()
			cancel = nil
		}
	}
}
