package api

type Client interface {
	Ping() error
	AllocateContainer(allocationGuid string, request ContainerAllocationRequest) (Container, error)
	GetContainer(allocationGuid string) (Container, error)
	InitializeContainer(allocationGuid string, request ContainerInitializationRequest) (Container, error)
	Run(allocationGuid string, request ContainerRunRequest) error
	DeleteContainer(allocationGuid string) error
	ListContainers() ([]Container, error)
	RemainingResources() (ExecutorResources, error)
	TotalResources() (ExecutorResources, error)
}
