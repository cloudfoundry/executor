package allocations

import (
	"errors"
	"fmt"
	"sync"

	garden "github.com/cloudfoundry-incubator/garden/api"
)

var (
	ErrNotImplemented         = errors.New("not implemented")
	ErrContainerAlreadyExists = errors.New("Container already exists")
)

type client struct {
	containers map[string]garden.Container
	mutex      sync.RWMutex
}

func NewClient() garden.Client {
	return &client{
		containers: make(map[string]garden.Container),
	}
}

func (client) Capacity() (garden.Capacity, error) {
	return garden.Capacity{}, ErrNotImplemented
}

func (client) Ping() error {
	return nil
}

func (c *client) Containers(filter garden.Properties) ([]garden.Container, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	containers := make([]garden.Container, 0, len(c.containers))

on:
	for _, container := range c.containers {
		for k, v := range filter {
			value, err := container.GetProperty(k)
			if err != nil || value != v {
				continue on
			}
		}

		containers = append(containers, container)
	}

	return containers, nil
}

func (c *client) Create(spec garden.ContainerSpec) (garden.Container, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if _, ok := c.containers[spec.Handle]; ok {
		return nil, ErrContainerAlreadyExists
	}

	if spec.Handle == "" {
		panic("no handle specified!")
	}

	container := newContainer(spec)
	c.containers[spec.Handle] = container
	return container, nil
}

func (c *client) Destroy(handle string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	_, found := c.containers[handle]
	if !found {
		return fmt.Errorf("container not allocated for handle: %s", handle)
	}

	delete(c.containers, handle)

	return nil
}

func (c *client) Lookup(handle string) (garden.Container, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	container, found := c.containers[handle]
	if !found {
		return nil, fmt.Errorf("container not allocated for handle: %s", handle)
	}

	return container, nil
}
