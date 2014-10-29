package allocations

import (
	"errors"

	garden "github.com/cloudfoundry-incubator/garden/api"
)

var ErrNotImplemented = errors.New("not implemented")

type client struct{}

func NewClient() garden.Client {
	return client{}
}

func (client) Capacity() (garden.Capacity, error) {
	return garden.Capacity{}, ErrNotImplemented
}

func (client) Ping() error {
	return ErrNotImplemented
}

func (client) Containers(garden.Properties) ([]garden.Container, error) {
	return nil, ErrNotImplemented
}

func (client) Create(spec garden.ContainerSpec) (garden.Container, error) {
	return newContainer(spec), nil
}

func (client) Destroy(string) error {
	return ErrNotImplemented
}

func (client) Lookup(string) (garden.Container, error) {
	return nil, ErrNotImplemented
}
