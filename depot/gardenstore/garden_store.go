package gardenstore

import "github.com/cloudfoundry-incubator/garden"

type GardenStore struct {
	gardenClient garden.Client
}

func NewGardenStore(gardenClient garden.Client) *GardenStore {
	return &GardenStore{gardenClient: gardenClient}
}

func (store *GardenStore) Ping() error {
	return store.gardenClient.Ping()
}
