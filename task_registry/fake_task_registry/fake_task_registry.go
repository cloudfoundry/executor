package fake_task_registry

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

func (fakeRegistry *FakeTaskRegistry) WriteToDisk() error { return nil }
