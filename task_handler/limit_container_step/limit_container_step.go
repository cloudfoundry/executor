package limit_container_step

import (
	"errors"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	steno "github.com/cloudfoundry/gosteno"
)

var ErrPercentOutOfBounds = errors.New("percentage must be between 0 and 100")

type LimitContainerStep struct {
	task                  *models.Task
	logger                *steno.Logger
	containerInodeLimit   int
	containerMaxCpuShares int
	container             *warden.Container
}

func New(
	task *models.Task,
	logger *steno.Logger,
	containerInodeLimit int,
	containerMaxCpuShares int,
	container *warden.Container,
) *LimitContainerStep {
	return &LimitContainerStep{
		task:                  task,
		logger:                logger,
		containerInodeLimit:   containerInodeLimit,
		containerMaxCpuShares: containerMaxCpuShares,
		container:             container,
	}
}

func (step LimitContainerStep) Perform() error {
	if step.task.CpuPercent < 0.0 || step.task.CpuPercent > 100.0 {
		return ErrPercentOutOfBounds
	}

	var err error

	if step.task.MemoryMB > 0 {
		err = (*step.container).LimitMemory(warden.MemoryLimits{
			LimitInBytes: uint64(step.task.MemoryMB * 1024 * 1024),
		})
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
	}

	err = (*step.container).LimitDisk(warden.DiskLimits{
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
		err = (*step.container).LimitCPU(warden.CPULimits{step.cpuShares()})
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
