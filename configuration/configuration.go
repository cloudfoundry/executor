package configuration

import (
	"fmt"
	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"strconv"
)

const Automatic = "auto"

type Configuration struct {
	wardenClient WardenClient.Client
	capacity     *warden.Capacity
}

func New(wardenClient WardenClient.Client) Configuration {
	return Configuration{
		wardenClient: wardenClient,
	}
}

func (config *Configuration) GetMemoryInMB(memoryMBFlag string) (int, error) {
	if memoryMBFlag == Automatic {
		capacity, err := config.getCapacity()
		if err != nil {
			return 0, err
		}
		return int(capacity.MemoryInBytes / (1024 * 1024)), nil
	} else {
		memoryMB, err := strconv.Atoi(memoryMBFlag)
		if err != nil || memoryMB <= 0 {
			return 0, fmt.Errorf("memory limit must be a positive number or '%s'", Automatic)
		}
		return memoryMB, nil
	}
}

func (config *Configuration) GetDiskInMB(diskMBFlag string) (int, error) {
	if diskMBFlag == Automatic {
		capacity, err := config.getCapacity()
		if err != nil {
			return 0, err
		}
		return int(capacity.DiskInBytes / (1024 * 1024)), nil
	} else {
		diskMB, err := strconv.Atoi(diskMBFlag)
		if err != nil || diskMB <= 0 {
			return 0, fmt.Errorf("disk limit must be a positive number or '%s'", Automatic)
		}
		return diskMB, nil
	}
}

func (config *Configuration) getCapacity() (warden.Capacity, error) {
	if config.capacity == nil {
		capacity, err := config.wardenClient.Capacity()
		config.capacity = &capacity
		return capacity, err
	}
	return *config.capacity, nil
}
