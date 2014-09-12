package main

import "github.com/cloudfoundry-incubator/garden/warden"

type executorContainers struct {
	wardenClient warden.Client
	owner        string
}

func (containers *executorContainers) Containers() ([]warden.Container, error) {
	return containers.wardenClient.Containers(warden.Properties{
		"owner": containers.owner,
	})
}
