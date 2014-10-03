package configuration

import (
	"fmt"
	"strconv"

	"github.com/cloudfoundry-incubator/executor/registry"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	garden_client "github.com/cloudfoundry-incubator/garden/client"
)

const Automatic = "auto"

var (
	ErrMemoryFlagInvalid = fmt.Errorf("memory limit must be a positive number or '%s'", Automatic)
	ErrDiskFlagInvalid   = fmt.Errorf("disk limit must be a positive number or '%s'", Automatic)
	EmptyCapacity        = registry.Capacity{}
)

func ConfigureCapacity(
	gardenClient garden_client.Client,
	memoryMBFlag string,
	diskMBFlag string,
) (registry.Capacity, error) {

	gardenCapacity, err := gardenClient.Capacity()
	if err != nil {
		return EmptyCapacity, err
	}

	memory, err := memoryInMB(gardenCapacity, memoryMBFlag)
	if err != nil {
		return EmptyCapacity, err
	}

	disk, err := diskInMB(gardenCapacity, diskMBFlag)
	if err != nil {
		return EmptyCapacity, err
	}

	return registry.Capacity{
		MemoryMB:   memory,
		DiskMB:     disk,
		Containers: int(gardenCapacity.MaxContainers),
	}, nil
}

func memoryInMB(capacity garden_api.Capacity, memoryMBFlag string) (int, error) {
	if memoryMBFlag == Automatic {
		return int(capacity.MemoryInBytes / (1024 * 1024)), nil
	} else {
		memoryMB, err := strconv.Atoi(memoryMBFlag)
		if err != nil || memoryMB <= 0 {
			return 0, ErrMemoryFlagInvalid
		}
		return memoryMB, nil
	}
}

func diskInMB(capacity garden_api.Capacity, diskMBFlag string) (int, error) {
	if diskMBFlag == Automatic {
		return int(capacity.DiskInBytes / (1024 * 1024)), nil
	} else {
		diskMB, err := strconv.Atoi(diskMBFlag)
		if err != nil || diskMB <= 0 {
			return 0, ErrDiskFlagInvalid
		}
		return diskMB, nil
	}
}
