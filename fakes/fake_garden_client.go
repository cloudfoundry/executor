package fakes

import (
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/garden/client"
	"code.cloudfoundry.org/garden/client/connection/fakes"
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
