package vcontainer

import (
	"code.cloudfoundry.org/garden"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
)

func ConvertProcessSpec(spec garden.ProcessSpec) (*vcontainermodels.ProcessSpec, error) {
	converted := vcontainermodels.ProcessSpec{
		ID:   spec.ID,
		Args: spec.Args,
		Path: spec.Path,
		Env:  spec.Env,
		Dir:  spec.Dir,
		User: spec.User,
	}
	for _, bindMount := range spec.BindMounts {
		converted.BindMounts = append(converted.BindMounts, vcontainermodels.BindMount{
			SrcPath: bindMount.SrcPath,
			DstPath: bindMount.DstPath,
			Mode:    vcontainermodels.BindMountMode(bindMount.Mode),
			Origin:  vcontainermodels.BindMountOrigin(bindMount.Origin),
		})
	}
	return &converted, nil
}

func ConvertContainerSpec(spec garden.ContainerSpec) (*vcontainermodels.ContainerSpec, error) {
	protoDuration := &google_protobuf.Duration{}
	protoDuration.Seconds = int64(spec.GraceTime) / 1e9
	protoDuration.Nanos = int32(int64(spec.GraceTime) % 1e9)
	converted := vcontainermodels.ContainerSpec{
		Handle:     spec.Handle,
		GraceTime:  protoDuration,
		RootFSPath: spec.RootFSPath,
		Image: vcontainermodels.ImageRef{
			URI:      spec.Image.URI,
			Username: spec.Image.Username,
			Password: spec.Image.Password,
		},
		Network:    spec.Network,
		Env:        spec.Env,
		Privileged: spec.Privileged,
	}

	converted.Properties = &vcontainermodels.Properties{
		Properties: spec.Properties,
	}

	converted.Limits = vcontainermodels.Limits{
		CPU: vcontainermodels.CPULimits{
			LimitInShares: spec.Limits.CPU.LimitInShares,
		},
		Memory: vcontainermodels.MemoryLimits{
			LimitInBytes: spec.Limits.Memory.LimitInBytes,
		},
	}

	for _, bindMount := range spec.BindMounts {
		converted.BindMounts = append(converted.BindMounts, vcontainermodels.BindMount{
			SrcPath: bindMount.SrcPath,
			DstPath: bindMount.DstPath,
			Mode:    vcontainermodels.BindMountMode(bindMount.Mode),
			Origin:  vcontainermodels.BindMountOrigin(bindMount.Origin),
		})
	}

	for _, netIn := range spec.NetIn {
		converted.NetIn = append(converted.NetIn, vcontainermodels.NetInRequest{
			HostPort:      netIn.HostPort,
			ContainerPort: netIn.ContainerPort,
		})
	}

	for _, netOut := range spec.NetOut {
		converted.NetOut = append(converted.NetOut, ConvertNetOutRule(&netOut))
	}

	return &converted, nil
}

func ConvertNetOutRule(origin *garden.NetOutRule) vcontainermodels.NetOutRuleRequest {
	return vcontainermodels.NetOutRuleRequest{
		Log: origin.Log,
	}
}

func ConvertProperties(properties *vcontainermodels.Properties) garden.Properties {
	if properties == nil {
		return nil
	} else {
		return garden.Properties(properties.Properties)
	}
}

func ConvertMappedPort(ports []vcontainermodels.PortMapping) []garden.PortMapping {
	if ports == nil {
		return nil
	} else {
		converted := make([]garden.PortMapping, len(ports))
		for i, port := range ports {
			converted[i] = garden.PortMapping{
				HostPort:      port.HostPort,
				ContainerPort: port.ContainerPort,
			}
		}
		return converted
	}
}

func ConvertContainerInfo(info vcontainermodels.ContainerInfo) garden.ContainerInfo {
	var properties map[string]string
	if info.Properties != nil {
		properties = info.Properties.Properties
	}
	if info.State == "Running" {
		info.State = "running"
	}
	return garden.ContainerInfo{
		State:         info.State,                          //string        // Either "active" or "stopped".
		Events:        nil,                                 // List of events that occurred for the container. It currently includes only "oom" (Out Of Memory) event if it occurred.
		HostIP:        info.HostIP,                         // The IP address of the gateway which controls the host side of the container's virtual ethernet pair.
		ContainerIP:   info.ContainerIP,                    // The IP address of the container side of the container's virtual ethernet pair.
		ExternalIP:    info.ExternalIP,                     //
		ContainerPath: info.ContainerPath,                  // The path to the directory holding the container's files (both its control scripts and filesystem).
		ProcessIDs:    nil,                                 //[]string      // List of running processes.
		Properties:    properties,                          // List of properties defined for the container.
		MappedPorts:   ConvertMappedPort(info.MappedPorts), //info.MappedPorts,           //
	}
}
