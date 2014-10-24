package executor

import "io"

type Client interface {
	Ping() error
	AllocateContainer(allocationGuid string, request ContainerAllocationRequest) (Container, error)
	GetContainer(allocationGuid string) (Container, error)
	InitializeContainer(allocationGuid string) (Container, error)
	Run(allocationGuid string, request ContainerRunRequest) error
	DeleteContainer(allocationGuid string) error
	ListContainers() ([]Container, error)
	RemainingResources() (ExecutorResources, error)
	TotalResources() (ExecutorResources, error)
	GetFiles(allocationGuid string, path string) (io.ReadCloser, error)
}
