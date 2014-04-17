package run_once_handler

import (
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"

	"github.com/cloudfoundry-incubator/gordon"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/claim_step"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/create_container_step"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/execute_step"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/limit_container_step"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/register_step"
	"github.com/cloudfoundry-incubator/executor/run_once_handler/start_step"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/sequence/lazy_sequence"
	"github.com/cloudfoundry-incubator/executor/task_registry"
)

type RunOnceHandlerInterface interface {
	RunOnce(runOnce *models.RunOnce, executorId string, cancel <-chan struct{})
}

type RunOnceHandler struct {
	bbs                 Bbs.ExecutorBBS
	wardenClient        gordon.Client
	containerOwnerName  string
	transformer         *run_once_transformer.RunOnceTransformer
	logStreamerFactory  log_streamer_factory.LogStreamerFactory
	logger              *steno.Logger
	taskRegistry        task_registry.TaskRegistryInterface
	containerInodeLimit int
}

func New(
	bbs Bbs.ExecutorBBS,
	wardenClient gordon.Client,
	containerOwnerName string,
	taskRegistry task_registry.TaskRegistryInterface,
	transformer *run_once_transformer.RunOnceTransformer,
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
	logger *steno.Logger,
	containerInodeLimit int,
) *RunOnceHandler {
	return &RunOnceHandler{
		bbs:                 bbs,
		wardenClient:        wardenClient,
		containerOwnerName:  containerOwnerName,
		taskRegistry:        taskRegistry,
		transformer:         transformer,
		logStreamerFactory:  logStreamerFactory,
		logger:              logger,
		containerInodeLimit: containerInodeLimit,
	}
}

func (handler *RunOnceHandler) Cleanup() error {
	res, err := handler.wardenClient.List(map[string]string{
		"owner": handler.containerOwnerName,
	})
	if err != nil {
		return err
	}

	for _, handle := range res.GetHandles() {
		handler.logger.Infod(
			map[string]interface{}{
				"handle": handle,
			},
			"executor.cleanup",
		)

		_, err := handler.wardenClient.Destroy(handle)
		if err != nil {
			return err
		}
	}

	return nil
}

func (handler *RunOnceHandler) RunOnce(runOnce *models.RunOnce, executorID string, cancel <-chan struct{}) {
	var containerHandle string
	var runOnceResult string
	runner := sequence.New([]sequence.Step{
		register_step.New(
			runOnce,
			handler.logger,
			handler.taskRegistry,
		),
		claim_step.New(
			runOnce,
			handler.logger,
			executorID,
			handler.bbs,
		),
		create_container_step.New(
			runOnce,
			handler.logger,
			handler.wardenClient,
			handler.containerOwnerName,
			&containerHandle,
		),
		limit_container_step.New(
			runOnce,
			handler.logger,
			handler.wardenClient,
			handler.containerInodeLimit,
			&containerHandle,
		),
		start_step.New(
			runOnce,
			handler.logger,
			handler.bbs,
			&containerHandle,
		),
		execute_step.New(
			runOnce,
			handler.logger,
			lazy_sequence.New(func() []sequence.Step {
				return handler.transformer.StepsFor(runOnce, containerHandle, &runOnceResult)
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
