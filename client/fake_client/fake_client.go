package fake_client

import (
	"github.com/cloudfoundry-incubator/executor/client"
)

type FakeClient struct {
	WhenAllocatingContainer   func(client.ContainerRequest) (client.ContainerResponse, error)
	WhenInitializingContainer func(allocationGuid string) error
	WhenRunning               func(allocationGuid string, request client.RunRequest) error
	WhenDeletingContainer     func(allocationGuid string) error
}

func New() *FakeClient {
	return &FakeClient{}
}

func (c *FakeClient) AllocateContainer(request client.ContainerRequest) (client.ContainerResponse, error) {
	return c.WhenAllocatingContainer(request)
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
