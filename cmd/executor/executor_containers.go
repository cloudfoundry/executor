package main

import (
	garden_api "github.com/cloudfoundry-incubator/garden/api"
)

type executorContainers struct {
	gardenClient garden_api.Client
	owner        string
}

func (containers *executorContainers) Containers() ([]garden_api.Container, error) {
	return containers.gardenClient.Containers(garden_api.Properties{
		"executor:owner": containers.owner,
	})
}
