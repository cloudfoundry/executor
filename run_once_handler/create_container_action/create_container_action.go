package create_container_action

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ContainerAction struct {
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
) *ContainerAction {
	return &ContainerAction{
		runOnce:         runOnce,
		logger:          logger,
		wardenClient:    wardenClient,
		containerHandle: containerHandle,
	}
}

func (action ContainerAction) Perform() error {
	createResponse, err := action.wardenClient.Create()
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-create.failed",
		)

		return err
	}

	*action.containerHandle = createResponse.GetHandle()

	return nil
}

func (action ContainerAction) Cancel() {}

func (action ContainerAction) Cleanup() {
	_, err := action.wardenClient.Destroy(*action.containerHandle)
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"handle":       action.runOnce.ContainerHandle,
				"error":        err.Error(),
			},
			"runonce.container-destroy.failed",
		)
	}
}
