package faketaskregistry

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeTaskRegistry struct {
	RegisteredRunOnces   []models.RunOnce
	UnregisteredRunOnces []models.RunOnce
	AddRunOnceErr        error
}

func New() *FakeTaskRegistry {
	return &FakeTaskRegistry{}
}

func (fakeRegistry *FakeTaskRegistry) AddRunOnce(runOnce models.RunOnce) error {
	if fakeRegistry.AddRunOnceErr == nil {
		fakeRegistry.RegisteredRunOnces = append(fakeRegistry.RegisteredRunOnces, runOnce)
	}
	return fakeRegistry.AddRunOnceErr
}

func (fakeRegistry *FakeTaskRegistry) RemoveRunOnce(runOnce models.RunOnce) {
	fakeRegistry.UnregisteredRunOnces = append(fakeRegistry.UnregisteredRunOnces, runOnce)
}

func (fakeRegistry *FakeTaskRegistry) HasCapacityForRunOnce(runOnce models.RunOnce) bool { return false }
func (fakeRegistry *FakeTaskRegistry) AvailableMemoryMB() int                            { return 0 }
func (fakeRegistry *FakeTaskRegistry) AvailableDiskMB() int                              { return 0 }
func (fakeRegistry *FakeTaskRegistry) WriteToDisk() error                                { return nil }
