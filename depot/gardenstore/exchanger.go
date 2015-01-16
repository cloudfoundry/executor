package gardenstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/lager"
)

type GardenClient interface {
	Create(garden.ContainerSpec) (garden.Container, error)
	Destroy(handle string) error
	Containers(garden.Properties) ([]garden.Container, error)
	Lookup(handle string) (garden.Container, error)
}

type Exchanger interface {
	Garden2Executor(garden.Container) (executor.Container, error)
	CreateInGarden(lager.Logger, GardenClient, executor.Container) (executor.Container, error)
}

const (
	tagPropertyPrefix      = "tag:"
	executorPropertyPrefix = "executor:"

	ContainerOwnerProperty        = executorPropertyPrefix + "owner"
	ContainerStateProperty        = executorPropertyPrefix + "state"
	ContainerAllocatedAtProperty  = executorPropertyPrefix + "allocated-at"
	ContainerRootfsProperty       = executorPropertyPrefix + "rootfs"
	ContainerActionProperty       = executorPropertyPrefix + "action"
	ContainerSetupProperty        = executorPropertyPrefix + "setup"
	ContainerMonitorProperty      = executorPropertyPrefix + "monitor"
	ContainerEnvProperty          = executorPropertyPrefix + "env"
	ContainerLogProperty          = executorPropertyPrefix + "log"
	ContainerResultProperty       = executorPropertyPrefix + "result"
	ContainerMemoryMBProperty     = executorPropertyPrefix + "memory-mb"
	ContainerDiskMBProperty       = executorPropertyPrefix + "disk-mb"
	ContainerCPUWeightProperty    = executorPropertyPrefix + "cpu-weight"
	ContainerStartTimeoutProperty = executorPropertyPrefix + "start-timeout"
	ContainerEgressRulesProperty  = executorPropertyPrefix + "egress-rules"
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
		Guid:       gardenContainer.Handle(),
		Tags:       executor.Tags{},
		Ports:      make([]executor.PortMapping, len(info.MappedPorts)),
		ExternalIP: info.ExternalIP,
	}

	for key, value := range info.Properties {
		switch key {
		case ContainerStateProperty:
			state := executor.State(value)

			if state == executor.StateReserved ||
				state == executor.StateInitializing ||
				state == executor.StateCreated ||
				state == executor.StateRunning ||
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
		case ContainerSetupProperty:
			executorContainer.Setup, err = models.UnmarshalAction([]byte(value))
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerActionProperty:
			executorContainer.Action, err = models.UnmarshalAction([]byte(value))
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerMonitorProperty:
			executorContainer.Monitor, err = models.UnmarshalAction([]byte(value))
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
		case ContainerStartTimeoutProperty:
			startTimeout, err := strconv.Atoi(value)
			if err != nil {
				return executor.Container{}, MalformedPropertyError{
					Property: key,
					Value:    value,
				}
			}

			executorContainer.StartTimeout = uint(startTimeout)
		case ContainerEgressRulesProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.EgressRules)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}

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

func (exchanger exchanger) destroyContainer(logger lager.Logger, gardenClient GardenClient, gardenContainer garden.Container) {
	gardenErr := gardenClient.Destroy(gardenContainer.Handle())
	if gardenErr != nil {
		logger.Error("failed-destroy-garden-container", gardenErr)
	}
}

func (exchanger exchanger) CreateInGarden(logger lager.Logger, gardenClient GardenClient, executorContainer executor.Container) (executor.Container, error) {
	containerSpec := garden.ContainerSpec{
		Handle:     executorContainer.Guid,
		Privileged: executorContainer.Privileged,
		RootFSPath: executorContainer.RootFSPath,
	}

	setupJson, err := models.MarshalAction(executorContainer.Setup)
	if err != nil {
		logger.Error("failed-marshal-setup", err)
		return executor.Container{}, err
	}

	actionJson, err := models.MarshalAction(executorContainer.Action)
	if err != nil {
		logger.Error("failed-marshal-action", err)
		return executor.Container{}, err
	}

	monitorJson, err := models.MarshalAction(executorContainer.Monitor)
	if err != nil {
		logger.Error("failed-marshal-monitor", err)
		return executor.Container{}, err
	}

	envJson, err := json.Marshal(executorContainer.Env)
	if err != nil {
		logger.Error("failed-marshal-env", err)
		return executor.Container{}, err
	}

	logJson, err := json.Marshal(executorContainer.Log)
	if err != nil {
		logger.Error("failed-marshal-log", err)
		return executor.Container{}, err
	}

	resultJson, err := json.Marshal(executorContainer.RunResult)
	if err != nil {
		logger.Error("failed-marshal-run-result", err)
		return executor.Container{}, err
	}

	securityGroupRuleJson, err := json.Marshal(executorContainer.EgressRules)
	if err != nil {
		logger.Error("failed-marshal-egress-rules", err)
		return executor.Container{}, err
	}

	containerSpec.Properties = garden.Properties{
		ContainerOwnerProperty:        exchanger.containerOwnerName,
		ContainerStateProperty:        string(executorContainer.State),
		ContainerAllocatedAtProperty:  fmt.Sprintf("%d", executorContainer.AllocatedAt),
		ContainerStartTimeoutProperty: fmt.Sprintf("%d", executorContainer.StartTimeout),
		ContainerRootfsProperty:       executorContainer.RootFSPath,
		ContainerSetupProperty:        string(setupJson),
		ContainerActionProperty:       string(actionJson),
		ContainerMonitorProperty:      string(monitorJson),
		ContainerEnvProperty:          string(envJson),
		ContainerLogProperty:          string(logJson),
		ContainerResultProperty:       string(resultJson),
		ContainerMemoryMBProperty:     fmt.Sprintf("%d", executorContainer.MemoryMB),
		ContainerDiskMBProperty:       fmt.Sprintf("%d", executorContainer.DiskMB),
		ContainerCPUWeightProperty:    fmt.Sprintf("%d", executorContainer.CPUWeight),
		ContainerEgressRulesProperty:  string(securityGroupRuleJson),
	}

	for name, value := range executorContainer.Tags {
		containerSpec.Properties[tagPropertyPrefix+name] = value
	}

	for _, env := range executorContainer.Env {
		containerSpec.Env = append(containerSpec.Env, env.Name+"="+env.Value)
	}
	for _, securityRule := range executorContainer.EgressRules {
		if err := securityRule.Validate(); err != nil {
			logger.Error("invalid-security-rule", err, lager.Data{"security_group_rule": securityRule})
			return executor.Container{}, executor.ErrInvalidSecurityGroup
		}
	}

	gardenContainer, err := gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-create-garden", err)
		return executor.Container{}, err
	}

	if executorContainer.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(executorContainer.Ports))

		for i, ports := range executorContainer.Ports {
			actualHostPort, actualContainerPort, err := gardenContainer.NetIn(ports.HostPort, ports.ContainerPort)
			if err != nil {
				logger.Error("failed-setup-ports", err)
				exchanger.destroyContainer(logger, gardenClient, gardenContainer)
				return executor.Container{}, err
			}

			actualPortMappings[i].ContainerPort = actualContainerPort
			actualPortMappings[i].HostPort = actualHostPort
		}

		executorContainer.Ports = actualPortMappings
	}

	for _, securityRule := range executorContainer.EgressRules {
		var protocol garden.Protocol
		icmpType := int32(-1)
		icmpCode := int32(-1)
		switch securityRule.Protocol {
		case models.TCPProtocol:
			protocol = garden.ProtocolTCP
		case models.UDPProtocol:
			protocol = garden.ProtocolUDP
		case models.ICMPProtocol:
			protocol = garden.ProtocolICMP
			icmpType = securityRule.IcmpInfo.Type
			icmpCode = securityRule.IcmpInfo.Code
		case models.AllProtocol:
			protocol = garden.ProtocolAll
		}

		var portRange string
		if securityRule.PortRange != nil {
			portRange = fmt.Sprintf("%d:%d", securityRule.PortRange.Start, securityRule.PortRange.End)
		}

		err := gardenContainer.NetOut(securityRule.Destination, 0, portRange, protocol, icmpType, icmpCode)
		if err != nil {
			logger.Error("failed-to-net-out", err, lager.Data{"security_group_rule": securityRule})
			exchanger.destroyContainer(logger, gardenClient, gardenContainer)
			return executor.Container{}, err
		}
	}

	if executorContainer.MemoryMB != 0 {
		err := gardenContainer.LimitMemory(garden.MemoryLimits{
			LimitInBytes: uint64(executorContainer.MemoryMB * 1024 * 1024),
		})
		if err != nil {
			logger.Error("failed-setup-memory-limits", err)

			gardenErr := gardenClient.Destroy(gardenContainer.Handle())
			if gardenErr != nil {
				logger.Error("failed-destroy-garden-container", gardenErr)
			}

			return executor.Container{}, err
		}
	}

	err = gardenContainer.LimitDisk(garden.DiskLimits{
		ByteHard:  uint64(executorContainer.DiskMB * 1024 * 1024),
		InodeHard: exchanger.containerInodeLimit,
	})
	if err != nil {
		logger.Error("failed-setup-disk-limits", err)

		gardenErr := gardenClient.Destroy(gardenContainer.Handle())
		if gardenErr != nil {
			logger.Error("failed-destroy-garden-container", gardenErr)
		}

		return executor.Container{}, err
	}

	err = gardenContainer.LimitCPU(garden.CPULimits{
		LimitInShares: uint64(float64(exchanger.containerMaxCPUShares) * float64(executorContainer.CPUWeight) / 100.0),
	})
	if err != nil {
		logger.Error("failed-setup-cpu", err)

		gardenErr := gardenClient.Destroy(gardenContainer.Handle())
		if gardenErr != nil {
			logger.Error("failed-destroy-garden-container", gardenErr)
		}

		return executor.Container{}, err
	}

	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-garden-container-info", err)

		gardenErr := gardenClient.Destroy(gardenContainer.Handle())
		if gardenErr != nil {
			logger.Error("failed-destroy-garden-container", gardenErr)
		}

		return executor.Container{}, err
	}

	executorContainer.ExternalIP = info.ExternalIP

	return executorContainer, nil
}
