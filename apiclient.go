package executor

import "io"

type Client interface {
	Ping() error
	AllocateContainer(guid string, request Container) (Container, error)
	GetContainer(guid string) (Container, error)
	RunContainer(guid string) error
	DeleteContainer(guid string) error
	ListContainers() ([]Container, error)
	RemainingResources() (ExecutorResources, error)
	TotalResources() (ExecutorResources, error)
	GetFiles(guid string, path string) (io.ReadCloser, error)
}
