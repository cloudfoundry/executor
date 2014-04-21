package task_registry

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var ErrorNoStackDefined = errors.New("no stack was defined for Task")

type TaskRegistryInterface interface {
	AddTask(task *models.Task) error
	RemoveTask(task *models.Task)
}

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	Tasks         map[string]*models.Task
	lock             *sync.Mutex

	stack string
}

type IncompatibleStackError struct {
	Have string
	Want string
}

func (e IncompatibleStackError) Error() string {
	return fmt.Sprintf(
		"run once has incompatible stack: have %s, want %s",
		e.Have,
		e.Want,
	)
}

func NewTaskRegistry(stack string, memoryMB int, diskMB int) *TaskRegistry {
	return &TaskRegistry{
		ExecutorMemoryMB: memoryMB,
		ExecutorDiskMB:   diskMB,
		Tasks:         make(map[string]*models.Task),

		lock: &sync.Mutex{},

		stack: stack,
	}
}

func (registry *TaskRegistry) AddTask(task *models.Task) error {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	if !registry.hasCapacityForTask(task) {
		return fmt.Errorf("insufficient resources to claim run once: Desired %d (memory) %d (disk).  Have %d (memory) %d (disk).", task.MemoryMB, task.DiskMB, registry.availableMemoryMB(), registry.availableDiskMB())
	}

	if task.Stack == "" {
		return ErrorNoStackDefined
	}

	if task.Stack != registry.stack {
		return IncompatibleStackError{registry.stack, task.Stack}
	}

	registry.Tasks[task.Guid] = task

	return nil
}

func (registry *TaskRegistry) RemoveTask(task *models.Task) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	delete(registry.Tasks, task.Guid)
}

func (registry *TaskRegistry) hasCapacityForTask(task *models.Task) bool {
	if task.MemoryMB > registry.availableMemoryMB() {
		return false
	}

	if task.DiskMB > registry.availableDiskMB() {
		return false
	}

	return true
}

func (registry *TaskRegistry) availableMemoryMB() int {
	usedMemory := 0
	for _, r := range registry.Tasks {
		usedMemory = usedMemory + r.MemoryMB
	}
	return registry.ExecutorMemoryMB - usedMemory
}

func (registry *TaskRegistry) availableDiskMB() int {
	usedDisk := 0
	for _, r := range registry.Tasks {
		usedDisk = usedDisk + r.DiskMB
	}
	return registry.ExecutorDiskMB - usedDisk
}
