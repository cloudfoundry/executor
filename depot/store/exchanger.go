package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudfoundry-incubator/executor"
	garden "github.com/cloudfoundry-incubator/garden/api"
)

type GardenClient interface {
	Create(garden.ContainerSpec) (garden.Container, error)
	Destroy(handle string) error
	Containers(garden.Properties) ([]garden.Container, error)
	Lookup(handle string) (garden.Container, error)
}

type Exchanger interface {
	Garden2Executor(garden.Container) (executor.Container, error)
	Executor2Garden(GardenClient, executor.Container) (garden.Container, error)
}

const (
	tagPropertyPrefix      = "tag:"
	executorPropertyPrefix = "executor:"

	ContainerOwnerProperty       = executorPropertyPrefix + "owner"
	ContainerStateProperty       = executorPropertyPrefix + "state"
	ContainerAllocatedAtProperty = executorPropertyPrefix + "allocated-at"
	ContainerRootfsProperty      = executorPropertyPrefix + "rootfs"
	ContainerActionsProperty     = executorPropertyPrefix + "actions"
	ContainerEnvProperty         = executorPropertyPrefix + "env"
	ContainerLogProperty         = executorPropertyPrefix + "log"
	ContainerResultProperty      = executorPropertyPrefix + "result"
	ContainerMemoryMBProperty    = executorPropertyPrefix + "memory-mb"
	ContainerDiskMBProperty      = executorPropertyPrefix + "disk-mb"
	ContainerCPUWeightProperty   = executorPropertyPrefix + "cpu-weight"
)

func NewExchanger(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
) Exchanger {
	return exchanger{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		containerInodeLimit:   containerInodeLimit,
	}
}

type exchanger struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	containerInodeLimit   uint64
}

func (exchanger exchanger) Garden2Executor(gardenContainer garden.Container) (executor.Container, error) {
	info, err := gardenContainer.Info()
	if err != nil {
		return executor.Container{}, err
	}

	executorContainer := executor.Container{
		Guid:  gardenContainer.Handle(),
		Tags:  executor.Tags{},
		Ports: make([]executor.PortMapping, len(info.MappedPorts)),
	}

	for key, value := range info.Properties {
		switch key {
		case ContainerStateProperty:
			state := executor.State(value)

			if state == executor.StateReserved ||
				state == executor.StateInitializing ||
				state == executor.StateCreated ||
				state == executor.StateCompleted {
				executorContainer.State = state
			} else {
				return executor.Container{}, InvalidStateError{value}
			}
		case ContainerAllocatedAtProperty:
			_, err := fmt.Sscanf(value, "%d", &executorContainer.AllocatedAt)
			if err != nil {
				return executor.Container{}, MalformedPropertyError{
					Property: ContainerAllocatedAtProperty,
					Value:    value,
				}
			}
		case ContainerRootfsProperty:
			executorContainer.RootFSPath = value
		case ContainerActionsProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.Actions)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerEnvProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.Env)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerLogProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.Log)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerResultProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.RunResult)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerMemoryMBProperty:
			memoryMB, err := strconv.Atoi(value)
			if err != nil {
				return executor.Container{}, MalformedPropertyError{
					Property: key,
					Value:    value,
				}
			}

			executorContainer.MemoryMB = memoryMB
		case ContainerDiskMBProperty:
			diskMB, err := strconv.Atoi(value)
			if err != nil {
				return executor.Container{}, MalformedPropertyError{
					Property: key,
					Value:    value,
				}
			}

			executorContainer.DiskMB = diskMB
		case ContainerCPUWeightProperty:
			cpuWeight, err := strconv.Atoi(value)
			if err != nil {
				return executor.Container{}, MalformedPropertyError{
					Property: key,
					Value:    value,
				}
			}

			executorContainer.CPUWeight = uint(cpuWeight)
		default:
			if strings.HasPrefix(key, tagPropertyPrefix) {
				executorContainer.Tags[key[len(tagPropertyPrefix):]] = value
			}
		}
	}

	for i, mapping := range info.MappedPorts {
		executorContainer.Ports[i] = executor.PortMapping{
			HostPort:      mapping.HostPort,
			ContainerPort: mapping.ContainerPort,
		}
	}

	return executorContainer, nil
}

func (exchanger exchanger) Executor2Garden(gardenClient GardenClient, executorContainer executor.Container) (garden.Container, error) {
	containerSpec := garden.ContainerSpec{
		Handle:     executorContainer.Guid,
		RootFSPath: executorContainer.RootFSPath,
	}

	actionsJson, err := json.Marshal(executorContainer.Actions)
	if err != nil {
		return nil, err
	}

	envJson, err := json.Marshal(executorContainer.Env)
	if err != nil {
		return nil, err
	}

	logJson, err := json.Marshal(executorContainer.Log)
	if err != nil {
		return nil, err
	}

	resultJson, err := json.Marshal(executorContainer.RunResult)
	if err != nil {
		return nil, err
	}

	containerSpec.Properties = garden.Properties{
		ContainerOwnerProperty:       exchanger.containerOwnerName,
		ContainerStateProperty:       string(executorContainer.State),
		ContainerAllocatedAtProperty: fmt.Sprintf("%d", executorContainer.AllocatedAt),
		ContainerRootfsProperty:      executorContainer.RootFSPath,
		ContainerActionsProperty:     string(actionsJson),
		ContainerEnvProperty:         string(envJson),
		ContainerLogProperty:         string(logJson),
		ContainerResultProperty:      string(resultJson),
		ContainerMemoryMBProperty:    fmt.Sprintf("%d", executorContainer.MemoryMB),
		ContainerDiskMBProperty:      fmt.Sprintf("%d", executorContainer.DiskMB),
		ContainerCPUWeightProperty:   fmt.Sprintf("%d", executorContainer.CPUWeight),
	}

	for name, value := range executorContainer.Tags {
		containerSpec.Properties[tagPropertyPrefix+name] = value
	}

	gardenContainer, err := gardenClient.Create(containerSpec)
	if err != nil {
		return nil, err
	}

	for _, ports := range executorContainer.Ports {
		_, _, err := gardenContainer.NetIn(ports.HostPort, ports.ContainerPort)
		if err != nil {
			return nil, err
		}
	}

	if executorContainer.MemoryMB != 0 {
		err := gardenContainer.LimitMemory(garden.MemoryLimits{
			LimitInBytes: uint64(executorContainer.MemoryMB * 1024 * 1024),
		})
		if err != nil {
			return nil, err
		}
	}

	err = gardenContainer.LimitDisk(garden.DiskLimits{
		ByteHard:  uint64(executorContainer.DiskMB * 1024 * 1024),
		InodeHard: exchanger.containerInodeLimit,
	})
	if err != nil {
		return nil, err
	}

	err = gardenContainer.LimitCPU(garden.CPULimits{
		LimitInShares: uint64(float64(exchanger.containerMaxCPUShares) * float64(executorContainer.CPUWeight) / 100.0),
	})
	if err != nil {
		return nil, err
	}

	return gardenContainer, nil
}
