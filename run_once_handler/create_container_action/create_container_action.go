package create_container_action

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ContainerAction struct {
	runOnce      *models.RunOnce
	logger       *steno.Logger
	wardenClient gordon.Client
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	wardenClient gordon.Client,
) *ContainerAction {
	return &ContainerAction{
		runOnce:      runOnce,
		logger:       logger,
		wardenClient: wardenClient,
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

	action.runOnce.ContainerHandle = createResponse.GetHandle()

	return nil
}

func (action ContainerAction) Cancel() {}

func (action ContainerAction) Cleanup() {
	_, err := action.wardenClient.Destroy(action.runOnce.ContainerHandle)
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
