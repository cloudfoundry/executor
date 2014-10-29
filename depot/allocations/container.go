package allocations

import (
	"fmt"
	"io"
	"sync"

	garden "github.com/cloudfoundry-incubator/garden/api"
)

type container struct {
	handle string

	properties garden.Properties

	memoryLimits garden.MemoryLimits
	diskLimits   garden.DiskLimits
	cpuLimits    garden.CPULimits

	mappedPorts []garden.PortMapping

	lock *sync.RWMutex
}

func newContainer(spec garden.ContainerSpec) garden.Container {
	properties := garden.Properties{}
	for k, v := range spec.Properties {
		properties[k] = v
	}

	return &container{
		handle: spec.Handle,

		properties: properties,

		lock: new(sync.RWMutex),
	}
}

func (c container) Handle() string {
	return c.handle
}

func (c *container) GetProperty(name string) (string, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	value, found := c.properties[name]
	if !found {
		return "", fmt.Errorf("property not defined: %s", name)
	}

	return value, nil
}

func (c *container) SetProperty(name string, value string) error {
	c.lock.Lock()
	c.properties[name] = value
	c.lock.Unlock()

	return ErrNotImplemented
}

func (c *container) RemoveProperty(name string) error {
	c.lock.Lock()

	_, found := c.properties[name]
	if !found {
		c.lock.Unlock()
		return fmt.Errorf("unknown property: %s", name)
	}

	delete(c.properties, name)

	c.lock.Unlock()

	return ErrNotImplemented
}

func (*container) Stop(kill bool) error {
	return ErrNotImplemented
}

func (c *container) Info() (garden.ContainerInfo, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	ports := make([]garden.PortMapping, len(c.mappedPorts))
	copy(ports, c.mappedPorts)

	properties := garden.Properties{}
	for k, v := range c.properties {
		properties[k] = v
	}

	return garden.ContainerInfo{
		Properties:  properties,
		MappedPorts: ports,
	}, nil
}

func (*container) StreamIn(dstPath string, tarStream io.Reader) error {
	return ErrNotImplemented
}

func (*container) StreamOut(srcPath string) (io.ReadCloser, error) {
	return nil, ErrNotImplemented
}

func (*container) LimitBandwidth(limits garden.BandwidthLimits) error {
	return ErrNotImplemented
}

func (*container) CurrentBandwidthLimits() (garden.BandwidthLimits, error) {
	return garden.BandwidthLimits{}, ErrNotImplemented
}

func (c *container) LimitCPU(limits garden.CPULimits) error {
	c.lock.Lock()
	c.cpuLimits = limits
	c.lock.Unlock()

	return nil
}

func (c *container) CurrentCPULimits() (garden.CPULimits, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.cpuLimits, nil
}

func (c *container) LimitDisk(limits garden.DiskLimits) error {
	c.lock.Lock()
	c.diskLimits = limits
	c.lock.Unlock()

	return nil
}

func (c *container) CurrentDiskLimits() (garden.DiskLimits, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.diskLimits, nil
}

func (c *container) LimitMemory(limits garden.MemoryLimits) error {
	c.lock.Lock()
	c.memoryLimits = limits
	c.lock.Unlock()

	return nil
}

func (c *container) CurrentMemoryLimits() (garden.MemoryLimits, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.memoryLimits, nil
}

func (c *container) NetIn(hostPort uint32, containerPort uint32) (uint32, uint32, error) {
	c.lock.Lock()

	c.mappedPorts = append(c.mappedPorts, garden.PortMapping{
		HostPort:      hostPort,
		ContainerPort: containerPort,
	})

	c.lock.Unlock()

	return hostPort, containerPort, nil
}

func (*container) NetOut(network string, port uint32) error {
	return ErrNotImplemented
}

func (*container) Run(arg1 garden.ProcessSpec, arg2 garden.ProcessIO) (garden.Process, error) {
	return nil, ErrNotImplemented
}

func (*container) Attach(arg1 uint32, arg2 garden.ProcessIO) (garden.Process, error) {
	return nil, ErrNotImplemented
}
