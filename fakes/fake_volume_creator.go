package fakes

import (
	"github.com/cloudfoundry-incubator/executor/volume"
	"github.com/nu7hatch/gouuid"
)

type FakeVolumeCreator struct{}

func NewFakeVolumeCreator() FakeVolumeCreator { return FakeVolumeCreator{} }

func (vc FakeVolumeCreator) Create(store string, spec volumes.VolumeSpec) (volumes.Volume, error) {
	id, err := uuid.NewV4()
	if err != nil {
		return volumes.Volume{}, err
	}

	v := volumes.Volume{
		Path:          spec.DesiredHostPath,
		TotalCapacity: spec.DesiredSize,
		Id:            id.String(),
	}

	return v, nil
}
