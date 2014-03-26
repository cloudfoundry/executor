package create_container_step

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ContainerStep struct {
	runOnce         *models.RunOnce
	logger          *steno.Logger
	wardenClient    gordon.Client
	containerHandle *string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	wardenClient gordon.Client,
	containerHandle *string,
) *ContainerStep {
	return &ContainerStep{
		runOnce:         runOnce,
		logger:          logger,
		wardenClient:    wardenClient,
		containerHandle: containerHandle,
	}
}

func (step ContainerStep) Perform() error {
	createResponse, err := step.wardenClient.Create()
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
