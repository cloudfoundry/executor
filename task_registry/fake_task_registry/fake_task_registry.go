package fake_task_registry

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeTaskRegistry struct {
	RegisteredTasks   []*models.Task
	UnregisteredTasks []*models.Task
	AddTaskErr        error
}

func New() *FakeTaskRegistry {
	return &FakeTaskRegistry{}
}

func (fakeRegistry *FakeTaskRegistry) AddTask(task *models.Task) error {
	if fakeRegistry.AddTaskErr == nil {
		fakeRegistry.RegisteredTasks = append(fakeRegistry.RegisteredTasks, task)
	}

	return fakeRegistry.AddTaskErr
}

func (fakeRegistry *FakeTaskRegistry) RemoveTask(task *models.Task) {
	fakeRegistry.UnregisteredTasks = append(fakeRegistry.UnregisteredTasks, task)
}

func (fakeRegistry *FakeTaskRegistry) WriteToDisk() error { return nil }
