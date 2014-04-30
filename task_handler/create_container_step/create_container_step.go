package create_container_step

import (
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ContainerStep struct {
	task               *models.Task
	logger             *steno.Logger
	wardenClient       warden.Client
	containerOwnerName string
	container          *warden.Container
}

func New(
	task *models.Task,
	logger *steno.Logger,
	wardenClient warden.Client,
	containerOwnerName string,
	container *warden.Container,
) *ContainerStep {
	return &ContainerStep{
		task:               task,
		logger:             logger,
		wardenClient:       wardenClient,
		containerOwnerName: containerOwnerName,
		container:          container,
	}
}

func (step ContainerStep) Perform() error {
	container, err := step.wardenClient.Create(warden.ContainerSpec{
		Properties: warden.Properties{
			"owner": step.containerOwnerName,
		},
	})

	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":     err.Error(),
			},
			"task.container-create.failed",
		)

		return err
	}

	*step.container = container

	return nil
}

func (step ContainerStep) Cancel() {}

func (step ContainerStep) Cleanup() {
	err := step.wardenClient.Destroy((*step.container).Handle())
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"handle":    step.task.ContainerHandle,
				"error":     err.Error(),
			},
			"task.container-destroy.failed",
		)
	}
}
