package limit_container_action

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ContainerAction struct {
	runOnce             *models.RunOnce
	logger              *steno.Logger
	wardenClient        gordon.Client
	containerInodeLimit int
	containerHandle     *string
}

func New(
	runOnce *models.RunOnce,
	logger *steno.Logger,
	wardenClient gordon.Client,
	containerInodeLimit int,
	containerHandle *string,
) *ContainerAction {
	return &ContainerAction{
		runOnce:             runOnce,
		logger:              logger,
		wardenClient:        wardenClient,
		containerInodeLimit: containerInodeLimit,
		containerHandle:     containerHandle,
	}
}

func (action ContainerAction) Perform() error {
	_, err := action.wardenClient.LimitMemory(*action.containerHandle, uint64(action.runOnce.MemoryMB*1024*1024))
	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-limit-memory.failed",
		)

		return err
	}

	_, err = action.wardenClient.LimitDisk(*action.containerHandle, gordon.DiskLimits{
		ByteLimit:  uint64(action.runOnce.DiskMB * 1024 * 1024),
		InodeLimit: uint64(action.containerInodeLimit),
	})

	if err != nil {
		action.logger.Errord(
			map[string]interface{}{
				"runonce-guid": action.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-limit-disk.failed",
		)

		return err
	}

	return nil
}

func (action ContainerAction) Cancel() {}

func (action ContainerAction) Cleanup() {}
