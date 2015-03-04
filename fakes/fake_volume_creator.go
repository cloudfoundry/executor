package fakes

import (
	"github.com/cloudfoundry-incubator/executor/volume"
)

type FakeVolumeCreator struct{}

func NewFakeVolumeCreator() FakeVolumeCreator { return FakeVolumeCreator{} }

func (vc FakeVolumeCreator) Create(spec volumes.VolumeSpec) error {
	return nil
}
