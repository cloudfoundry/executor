package main

import (
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/garden"
)

type executorContainers struct {
	gardenClient garden.Client
	owner        string
}

func (containers *executorContainers) Containers() ([]garden.Container, error) {
	return containers.gardenClient.Containers(garden.Properties{
		gardenstore.ContainerOwnerProperty: containers.owner,
	})
}
