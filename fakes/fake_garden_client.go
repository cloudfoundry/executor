package fakes

import (
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/garden/client"
	"github.com/cloudfoundry-incubator/garden/client/connection/fakes"
)

type FakeGardenClient struct {
	garden.Client

	Connection *fakes.FakeConnection
}

func NewGardenClient() *FakeGardenClient {
	connection := new(fakes.FakeConnection)

	return &FakeGardenClient{
		Connection: connection,

		Client: client.New(connection),
	}
}
