package limit_container_step

import (
	"errors"

	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

var ErrPercentOutOfBounds = errors.New("percentage must be between 0 and 100")

type LimitContainerStep struct {
	task                  *models.Task
	logger                *steno.Logger
	wardenClient          gordon.Client
	containerInodeLimit   int
	containerMaxCpuShares int
	containerHandle       *string
}

func New(
	task *models.Task,
	logger *steno.Logger,
	wardenClient gordon.Client,
	containerInodeLimit int,
	containerMaxCpuShares int,
	containerHandle *string,
) *LimitContainerStep {
	return &LimitContainerStep{
		task:                  task,
		logger:                logger,
		wardenClient:          wardenClient,
		containerInodeLimit:   containerInodeLimit,
		containerMaxCpuShares: containerMaxCpuShares,
		containerHandle:       containerHandle,
	}
}

func (step LimitContainerStep) Perform() error {
	if step.task.CpuPercent < 0.0 || step.task.CpuPercent > 100.0 {
		return ErrPercentOutOfBounds
	}

	_, err := step.wardenClient.LimitMemory(*step.containerHandle, uint64(step.task.MemoryMB*1024*1024))
	if err != nil {
		step.logger.Errord(
			map[string]interface{}{
				"task-guid": step.task.Guid,
				"error":     err.Error(),
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
				"error":     err.Error(),
			},
			"task.container-limit-disk.failed",
		)

		return err
	}

	if step.task.CpuPercent != 0 {
		_, err = step.wardenClient.LimitCPU(*step.containerHandle, step.cpuShares())

		if err != nil {
			step.logger.Errord(
				map[string]interface{}{
					"task-guid": step.task.Guid,
					"shares":    step.cpuShares(),
					"error":     err.Error(),
				},
				"task.container-limit-cpu.failed",
			)

			return err
		}
	}

	return nil
}

func (step LimitContainerStep) Cancel() {}

func (step LimitContainerStep) Cleanup() {}

func (step LimitContainerStep) cpuShares() uint64 {
	return uint64(step.task.CpuPercent / 100 * float64(step.containerMaxCpuShares))
}
