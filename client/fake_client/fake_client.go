package fake_client

import (
	"github.com/cloudfoundry-incubator/executor/client"
)

type FakeClient struct {
	WhenAllocatingContainer   func(allocationGuid string, request client.ContainerRequest) (client.ContainerResponse, error)
	WhenInitializingContainer func(allocationGuid string) error
	WhenRunning               func(allocationGuid string, request client.RunRequest) error
	WhenDeletingContainer     func(allocationGuid string) error
}

func New() *FakeClient {
	return &FakeClient{
		WhenAllocatingContainer: func(allocationGuid string, req client.ContainerRequest) (client.ContainerResponse, error) {
			return client.ContainerResponse{}, nil
		},
		WhenInitializingContainer: func(allocationGuid string) error {
			return nil
		},
		WhenRunning: func(allocationGuid string, request client.RunRequest) error {
			return nil
		},
		WhenDeletingContainer: func(allocationGuid string) error {
			return nil
		},
	}
}

func (c *FakeClient) AllocateContainer(allocationGuid string, request client.ContainerRequest) (client.ContainerResponse, error) {
	return c.WhenAllocatingContainer(allocationGuid, request)
}

func (c *FakeClient) InitializeContainer(allocationGuid string) error {
	return c.WhenInitializingContainer(allocationGuid)
}

func (c *FakeClient) Run(allocationGuid string, request client.RunRequest) error {
	return c.WhenRunning(allocationGuid, request)
}

func (c *FakeClient) DeleteContainer(allocationGuid string) error {
	return c.WhenDeletingContainer(allocationGuid)
}
