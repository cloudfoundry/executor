package gardenstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/lager"
)

type GardenClient interface {
	Create(garden.ContainerSpec) (garden.Container, error)
	Destroy(handle string) error
	Containers(garden.Properties) ([]garden.Container, error)
	Lookup(handle string) (garden.Container, error)
}

type Exchanger interface {
	Infos(lager.Logger, garden.Client, []garden.Container) ([]executor.Container, error)
	Info(lager.Logger, garden.Container) (executor.Container, error)
	CreateInGarden(lager.Logger, GardenClient, executor.Container) (executor.Container, error)
}

const (
	TagPropertyPrefix      = "tag:"
	executorPropertyPrefix = "executor:"

	ContainerOwnerProperty         = executorPropertyPrefix + "owner"
	ContainerStateProperty         = executorPropertyPrefix + "state"
	ContainerAllocatedAtProperty   = executorPropertyPrefix + "allocated-at"
	ContainerRootfsProperty        = executorPropertyPrefix + "rootfs"
	ContainerLogProperty           = executorPropertyPrefix + "log-config"
	ContainerMetricsConfigProperty = executorPropertyPrefix + "metrics-config"
	ContainerResultProperty        = executorPropertyPrefix + "result"
	ContainerMemoryMBProperty      = executorPropertyPrefix + "memory-mb"
	ContainerDiskMBProperty        = executorPropertyPrefix + "disk-mb"
	ContainerCPUWeightProperty     = executorPropertyPrefix + "cpu-weight"
	ContainerStartTimeoutProperty  = executorPropertyPrefix + "start-timeout"
)

type exchanger struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	containerInodeLimit   uint64
}

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

func (exchanger exchanger) Infos(logger lager.Logger, gardenClient garden.Client, gardenContainers []garden.Container) ([]executor.Container, error) {
	handles := make([]string, 0, len(gardenContainers))
	for _, c := range gardenContainers {
		handles = append(handles, c.Handle())
	}
	logger = logger.Session("infos", lager.Data{"handles": handles})

	logger.Debug("get-bulk-info")
	infos, err := gardenClient.BulkInfo(handles)
	if err != nil {
		logger.Error("failed-bulk-info", err)
		return []executor.Container{}, err
	}
	logger.Debug("succeeded-bulk-info")

	var containerInfos []executor.Container

	for handle, info := range infos {
		if info.Err != nil {
			logger.Error("failed-garden-info", info.Err, lager.Data{
				"handle": handle,
			})
			continue
		}

		container, err := garden2executor(handle, info.Info)
		if err != nil {
			logger.Error("failed-garden2executor", err, lager.Data{
				"handle": handle,
			})
			continue
		}

		containerInfos = append(containerInfos, container)
	}

	return containerInfos, nil
}

func (exchanger exchanger) Info(logger lager.Logger, gardenContainer garden.Container) (executor.Container, error) {
	logger = logger.Session("info", lager.Data{"handle": gardenContainer.Handle()})

	logger.Debug("getting-info")
	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-getting-info", err)
		return executor.Container{}, err
	}
	logger.Debug("succeeded-getting-info")

	return garden2executor(gardenContainer.Handle(), info)
}

func garden2executor(handle string, info garden.ContainerInfo) (executor.Container, error) {
	executorContainer := executor.Container{
		Guid:       handle,
		Tags:       executor.Tags{},
		ExternalIP: info.ExternalIP,
	}

	executorContainer.Ports = make([]executor.PortMapping, len(info.MappedPorts))

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
		case ContainerLogProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.LogConfig)
			if err != nil {
				return executor.Container{}, InvalidJSONError{
					Property:     key,
					Value:        value,
					UnmarshalErr: err,
				}
			}
		case ContainerMetricsConfigProperty:
			err := json.Unmarshal([]byte(value), &executorContainer.MetricsConfig)
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
		default:
			if strings.HasPrefix(key, TagPropertyPrefix) {
				executorContainer.Tags[key[len(TagPropertyPrefix):]] = value
			}
		}
	}

	for i, mapping := range info.MappedPorts {
		executorContainer.Ports[i] = executor.PortMapping{
			HostPort:      uint16(mapping.HostPort),
			ContainerPort: uint16(mapping.ContainerPort),
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
	logger = logger.Session("create-in-garden", lager.Data{"container-guid": executorContainer.Guid})
	containerSpec := garden.ContainerSpec{
		Handle:     executorContainer.Guid,
		Privileged: executorContainer.Privileged,
		RootFSPath: executorContainer.RootFSPath,
	}

	if executorContainer.MemoryMB != 0 {
		logger.Debug("setting-up-memory-limits")
		containerSpec.Limits.Memory.LimitInBytes = uint64(executorContainer.MemoryMB * 1024 * 1024)
	}

	logger.Debug("setting-up-disk-limits")
	gardenScope := garden.DiskLimitScopeExclusive
	if executorContainer.DiskScope == executor.TotalDiskLimit {
		gardenScope = garden.DiskLimitScopeTotal
	}
	containerSpec.Limits.Disk = garden.DiskLimits{
		ByteHard:  uint64(executorContainer.DiskMB * 1024 * 1024),
		InodeHard: exchanger.containerInodeLimit,
		Scope:     gardenScope,
	}

	logger.Debug("setting-up-cpu-limits")
	containerSpec.Limits.CPU.LimitInShares = uint64(float64(exchanger.containerMaxCPUShares) * float64(executorContainer.CPUWeight) / 100.0)

	logJson, err := json.Marshal(executorContainer.LogConfig)
	if err != nil {
		logger.Error("failed-marshal-log", err)
		return executor.Container{}, err
	}

	metricsConfigJson, err := json.Marshal(executorContainer.MetricsConfig)
	if err != nil {
		logger.Error("failed-marshal-metrics-config", err)
		return executor.Container{}, err
	}

	resultJson, err := json.Marshal(executorContainer.RunResult)
	if err != nil {
		logger.Error("failed-marshal-run-result", err)
		return executor.Container{}, err
	}

	containerSpec.Properties = garden.Properties{
		ContainerOwnerProperty:         exchanger.containerOwnerName,
		ContainerStateProperty:         string(executorContainer.State),
		ContainerAllocatedAtProperty:   fmt.Sprintf("%d", executorContainer.AllocatedAt),
		ContainerStartTimeoutProperty:  fmt.Sprintf("%d", executorContainer.StartTimeout),
		ContainerRootfsProperty:        executorContainer.RootFSPath,
		ContainerLogProperty:           string(logJson),
		ContainerMetricsConfigProperty: string(metricsConfigJson),
		ContainerResultProperty:        string(resultJson),
		ContainerMemoryMBProperty:      fmt.Sprintf("%d", executorContainer.MemoryMB),
		ContainerDiskMBProperty:        fmt.Sprintf("%d", executorContainer.DiskMB),
		ContainerCPUWeightProperty:     fmt.Sprintf("%d", executorContainer.CPUWeight),
	}

	for name, value := range executorContainer.Tags {
		containerSpec.Properties[TagPropertyPrefix+name] = value
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

	logger.Debug("creating-garden-container")
	gardenContainer, err := gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-creating-garden-container", err)
		return executor.Container{}, err
	}
	logger.Debug("succeeded-creating-garden-container")

	if executorContainer.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(executorContainer.Ports))

		logger.Debug("setting-up-ports")
		for i, ports := range executorContainer.Ports {
			actualHostPort, actualContainerPort, err := gardenContainer.NetIn(uint32(ports.HostPort), uint32(ports.ContainerPort))
			if err != nil {
				logger.Error("failed-setting-up-ports", err)
				exchanger.destroyContainer(logger, gardenClient, gardenContainer)
				return executor.Container{}, err
			}

			actualPortMappings[i].ContainerPort = uint16(actualContainerPort)
			actualPortMappings[i].HostPort = uint16(actualHostPort)
		}
		logger.Debug("succeeded-setting-up-ports")

		executorContainer.Ports = actualPortMappings
	}

	for _, securityRule := range executorContainer.EgressRules {
		netOutRule, err := securityGroupRuleToNetOutRule(securityRule)
		if err != nil {
			logger.Error("failed-to-build-net-out-rule", err, lager.Data{"security_group_rule": securityRule})
			return executor.Container{}, err
		}

		logger.Debug("setting-up-net-out")
		err = gardenContainer.NetOut(netOutRule)
		if err != nil {
			logger.Error("failed-setting-up-net-out", err, lager.Data{"net-out-rule": netOutRule})
			exchanger.destroyContainer(logger, gardenClient, gardenContainer)
			return executor.Container{}, err
		}
		logger.Debug("succeeded-setting-up-net-out")
	}

	logger.Debug("getting-garden-container-info")
	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-getting-garden-container-info", err)

		gardenErr := gardenClient.Destroy(gardenContainer.Handle())
		if gardenErr != nil {
			logger.Error("failed-destroy-garden-container", gardenErr)
		}

		return executor.Container{}, err
	}
	logger.Debug("succeeded-getting-garden-container-info")

	executorContainer.ExternalIP = info.ExternalIP

	return executorContainer, nil
}

func securityGroupRuleToNetOutRule(securityRule *models.SecurityGroupRule) (garden.NetOutRule, error) {
	var protocol garden.Protocol
	var portRanges []garden.PortRange
	var networks []garden.IPRange
	var icmp *garden.ICMPControl

	switch securityRule.Protocol {
	case models.TCPProtocol:
		protocol = garden.ProtocolTCP
	case models.UDPProtocol:
		protocol = garden.ProtocolUDP
	case models.ICMPProtocol:
		protocol = garden.ProtocolICMP
		icmp = &garden.ICMPControl{
			Type: garden.ICMPType(securityRule.IcmpInfo.Type),
			Code: garden.ICMPControlCode(uint8(securityRule.IcmpInfo.Code)),
		}
	case models.AllProtocol:
		protocol = garden.ProtocolAll
	}

	if securityRule.PortRange != nil {
		portRanges = append(portRanges, garden.PortRange{Start: uint16(securityRule.PortRange.Start), End: uint16(securityRule.PortRange.End)})
	} else if securityRule.Ports != nil {
		for _, port := range securityRule.Ports {
			portRanges = append(portRanges, garden.PortRangeFromPort(uint16(port)))
		}
	}

	for _, dest := range securityRule.Destinations {
		ipRange, err := toIPRange(dest)
		if err != nil {
			return garden.NetOutRule{}, err
		}
		networks = append(networks, ipRange)
	}

	netOutRule := garden.NetOutRule{
		Protocol: protocol,
		Networks: networks,
		Ports:    portRanges,
		ICMPs:    icmp,
		Log:      securityRule.Log,
	}

	return netOutRule, nil
}

var ErrIPRangeConversionFailed = errors.New("failed to convert destination to ip range")

func toIPRange(dest string) (garden.IPRange, error) {
	idx := strings.IndexAny(dest, "-/")

	// Not a range or a CIDR
	if idx == -1 {
		ip := net.ParseIP(dest)
		if ip == nil {
			return garden.IPRange{}, ErrIPRangeConversionFailed
		}

		return garden.IPRangeFromIP(ip), nil
	}

	// We have a CIDR
	if dest[idx] == '/' {
		_, ipNet, err := net.ParseCIDR(dest)
		if err != nil {
			return garden.IPRange{}, ErrIPRangeConversionFailed
		}

		return garden.IPRangeFromIPNet(ipNet), nil
	}

	// We have an IP range
	firstIP := net.ParseIP(dest[:idx])
	secondIP := net.ParseIP(dest[idx+1:])
	if firstIP == nil || secondIP == nil {
		return garden.IPRange{}, ErrIPRangeConversionFailed
	}

	return garden.IPRange{Start: firstIP, End: secondIP}, nil
}
