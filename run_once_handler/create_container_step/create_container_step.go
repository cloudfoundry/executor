package create_container_step

import (
	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

type ContainerStep struct {
	runOnce            *models.Task
	logger             *steno.Logger
	wardenClient       gordon.Client
	containerOwnerName string
	containerHandle    *string
}

func New(
	runOnce *models.Task,
	logger *steno.Logger,
	wardenClient gordon.Client,
	containerOwnerName string,
	containerHandle *string,
) *ContainerStep {
	return &ContainerStep{
		runOnce:            runOnce,
		logger:             logger,
		wardenClient:       wardenClient,
		containerOwnerName: containerOwnerName,
		containerHandle:    containerHandle,
	}
}

func (step ContainerStep) Perform() error {
	createResponse, err := step.wardenClient.Create(map[string]string{
		"owner": step.containerOwnerName,
	})

	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-create.failed",
		)

		return err
	}

	*step.containerHandle = createResponse.GetHandle()

	return nil
}

func (step ContainerStep) Cancel() {}

func (step ContainerStep) Cleanup() {
	_, err := step.wardenClient.Destroy(*step.containerHandle)
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"handle":       step.runOnce.ContainerHandle,
				"error":        err.Error(),
			},
			"runonce.container-destroy.failed",
		)
	}
}
