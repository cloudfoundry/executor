package task_handler

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/lazy_sequence"
	"github.com/cloudfoundry-incubator/executor/task_handler/claim_step"
	"github.com/cloudfoundry-incubator/executor/task_handler/create_container_step"
	"github.com/cloudfoundry-incubator/executor/task_handler/execute_step"
	"github.com/cloudfoundry-incubator/executor/task_handler/limit_container_step"
	"github.com/cloudfoundry-incubator/executor/task_handler/register_step"
	"github.com/cloudfoundry-incubator/executor/task_handler/start_step"
	"github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/executor/task_transformer"
)

type TaskHandlerInterface interface {
	Task(task *models.Task, executorId string, cancel <-chan struct{})
}

type TaskHandler struct {
	bbs                   Bbs.ExecutorBBS
	wardenClient          warden.Client
	containerOwnerName    string
	transformer           *task_transformer.TaskTransformer
	logStreamerFactory    log_streamer_factory.LogStreamerFactory
	logger                *steno.Logger
	taskRegistry          task_registry.TaskRegistryInterface
	containerInodeLimit   int
	containerMaxCpuShares int
}

func New(
	bbs Bbs.ExecutorBBS,
	wardenClient warden.Client,
	containerOwnerName string,
	taskRegistry task_registry.TaskRegistryInterface,
	transformer *task_transformer.TaskTransformer,
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	logger *steno.Logger,
	containerInodeLimit int,
	containerMaxCpuShares int,
) *TaskHandler {
	return &TaskHandler{
		bbs:                   bbs,
		wardenClient:          wardenClient,
		containerOwnerName:    containerOwnerName,
		taskRegistry:          taskRegistry,
		transformer:           transformer,
		logStreamerFactory:    logStreamerFactory,
		logger:                logger,
		containerInodeLimit:   containerInodeLimit,
		containerMaxCpuShares: containerMaxCpuShares,
	}
}

func (handler *TaskHandler) Cleanup() error {
	containers, err := handler.wardenClient.Containers(warden.Properties{
		"owner": handler.containerOwnerName,
	})
	if err != nil {
		return err
	}

	for _, container := range containers {
		handler.logger.Infod(
			map[string]interface{}{
				"handle": container.Handle(),
			},
			"executor.cleanup",
		)

		err := handler.wardenClient.Destroy(container.Handle())
		if err != nil {
			return err
		}
	}

	return nil
}

func (handler *TaskHandler) Task(task *models.Task, executorID string, cancel <-chan struct{}) {
	var container warden.Container
	var taskResult string

	runner := sequence.New([]sequence.Step{
		register_step.New(
			task,
			handler.logger,
			handler.taskRegistry,
		),
		claim_step.New(
			task,
			handler.logger,
			executorID,
			handler.bbs,
		),
		create_container_step.New(
			task,
			handler.logger,
			handler.wardenClient,
			handler.containerOwnerName,
			&container,
		),
		limit_container_step.New(
			task,
			handler.logger,
			handler.containerInodeLimit,
			handler.containerMaxCpuShares,
			&container,
		),
		start_step.New(
			task,
			handler.logger,
			handler.bbs,
			&container,
		),
		execute_step.New(
			task,
			handler.logger,
			lazy_sequence.New(func() []sequence.Step {
				return handler.transformer.StepsFor(task, container, &taskResult)
			}),
			handler.bbs,
			&taskResult,
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
