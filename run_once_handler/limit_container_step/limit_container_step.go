package limit_container_step

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/vito/gordon"
)

type ContainerStep struct {
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
) *ContainerStep {
	return &ContainerStep{
		runOnce:             runOnce,
		logger:              logger,
		wardenClient:        wardenClient,
		containerInodeLimit: containerInodeLimit,
		containerHandle:     containerHandle,
	}
}

func (step ContainerStep) Perform() error {
	_, err := step.wardenClient.LimitMemory(*step.containerHandle, uint64(step.runOnce.MemoryMB*1024*1024))
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-limit-memory.failed",
		)

		return err
	}

	_, err = step.wardenClient.LimitDisk(*step.containerHandle, gordon.DiskLimits{
		ByteLimit:  uint64(step.runOnce.DiskMB * 1024 * 1024),
		InodeLimit: uint64(step.containerInodeLimit),
	})

	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"runonce-guid": step.runOnce.Guid,
				"error":        err.Error(),
			},
			"runonce.container-limit-disk.failed",
		)

		return err
	}

	return nil
}

func (step ContainerStep) Cancel() {}

func (step ContainerStep) Cleanup() {}
