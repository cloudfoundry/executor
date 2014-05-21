package fake_client

import "github.com/cloudfoundry-incubator/executor/api"

type FakeClient struct {
	WhenAllocatingContainer        func(allocationGuid string, request api.ContainerAllocationRequest) (api.Container, error)
	WhenGettingContainer           func(allocationGuid string) (api.Container, error)
	WhenInitializingContainer      func(allocationGuid string) error
	WhenRunning                    func(allocationGuid string, request api.ContainerRunRequest) error
	WhenDeletingContainer          func(allocationGuid string) error
	WhenListingContainers          func() ([]api.Container, error)
	WhenFetchingRemainingResources func() (api.ExecutorResources, error)
	WhenFetchingTotalResources     func() (api.ExecutorResources, error)
}

func New() *FakeClient {
	return &FakeClient{
		WhenAllocatingContainer: func(allocationGuid string, req api.ContainerAllocationRequest) (api.Container, error) {
			return api.Container{}, nil
		},
		WhenGettingContainer: func(allocationGuid string) (api.Container, error) {
			return api.Container{}, nil
		},
		WhenInitializingContainer: func(allocationGuid string) error {
			return nil
		},
		WhenRunning: func(allocationGuid string, request api.ContainerRunRequest) error {
			return nil
		},
		WhenDeletingContainer: func(allocationGuid string) error {
			return nil
		},
		WhenListingContainers: func() ([]api.Container, error) {
			return nil, nil
		},
		WhenFetchingRemainingResources: func() (api.ExecutorResources, error) {
			return api.ExecutorResources{}, nil
		},
		WhenFetchingTotalResources: func() (api.ExecutorResources, error) {
			return api.ExecutorResources{}, nil
		},
	}
}

func (c *FakeClient) AllocateContainer(allocationGuid string, request api.ContainerAllocationRequest) (api.Container, error) {
	return c.WhenAllocatingContainer(allocationGuid, request)
}

func (c *FakeClient) GetContainer(allocationGuid string) (api.Container, error) {
	return c.WhenGettingContainer(allocationGuid)
}

func (c *FakeClient) InitializeContainer(allocationGuid string) error {
	return c.WhenInitializingContainer(allocationGuid)
}

func (c *FakeClient) Run(allocationGuid string, request api.ContainerRunRequest) error {
	return c.WhenRunning(allocationGuid, request)
}

func (c *FakeClient) DeleteContainer(allocationGuid string) error {
	return c.WhenDeletingContainer(allocationGuid)
}

func (c *FakeClient) ListContainers() ([]api.Container, error) {
	return c.WhenListingContainers()
}

func (c *FakeClient) RemainingResources() (api.ExecutorResources, error) {
	return c.WhenFetchingRemainingResources()
}

func (c *FakeClient) TotalResources() (api.ExecutorResources, error) {
	return c.WhenFetchingTotalResources()
}
