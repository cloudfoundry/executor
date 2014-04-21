package limit_container_step

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry-incubator/gordon"
)

type ContainerStep struct {
	task             *models.Task
	logger              *steno.Logger
	wardenClient        gordon.Client
	containerInodeLimit int
	containerHandle     *string
}

func New(
	task *models.Task,
	logger *steno.Logger,
	wardenClient gordon.Client,
	containerInodeLimit int,
	containerHandle *string,
) *ContainerStep {
	return &ContainerStep{
		task:             task,
		logger:              logger,
		wardenClient:        wardenClient,
		containerInodeLimit: containerInodeLimit,
		containerHandle:     containerHandle,
	}
}

func (step ContainerStep) Perform() error {
	_, err := step.wardenClient.LimitMemory(*step.containerHandle, uint64(step.task.MemoryMB*1024*1024))
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":        err.Error(),
			},
			"task.container-limit-memory.failed",
		)

		return err
	}

	_, err = step.wardenClient.LimitDisk(*step.containerHandle, gordon.DiskLimits{
		ByteLimit:  uint64(step.task.DiskMB * 1024 * 1024),
		InodeLimit: uint64(step.containerInodeLimit),
	})

	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":        err.Error(),
			},
			"task.container-limit-disk.failed",
		)

		return err
	}

	return nil
}

func (step ContainerStep) Cancel() {}

func (step ContainerStep) Cleanup() {}
