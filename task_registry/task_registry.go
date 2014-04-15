package task_registry

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var ErrorNoStackDefined = errors.New("no stack was defined for RunOnce")

type TaskRegistryInterface interface {
	AddRunOnce(runOnce *models.RunOnce) error
	RemoveRunOnce(runOnce *models.RunOnce)
}

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	RunOnces         map[string]*models.RunOnce
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
		RunOnces:         make(map[string]*models.RunOnce),

		lock: &sync.Mutex{},

		stack: stack,
	}
}

func (registry *TaskRegistry) AddRunOnce(runOnce *models.RunOnce) error {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	if !registry.hasCapacityForRunOnce(runOnce) {
		return fmt.Errorf("insufficient resources to claim run once: Desired %d (memory) %d (disk).  Have %d (memory) %d (disk).", runOnce.MemoryMB, runOnce.DiskMB, registry.availableMemoryMB(), registry.availableDiskMB())
	}

	if runOnce.Stack == "" {
		return ErrorNoStackDefined
	}

	if runOnce.Stack != registry.stack {
		return IncompatibleStackError{registry.stack, runOnce.Stack}
	}

	registry.RunOnces[runOnce.Guid] = runOnce

	return nil
}

func (registry *TaskRegistry) RemoveRunOnce(runOnce *models.RunOnce) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	delete(registry.RunOnces, runOnce.Guid)
}

func (registry *TaskRegistry) hasCapacityForRunOnce(runOnce *models.RunOnce) bool {
	if runOnce.MemoryMB > registry.availableMemoryMB() {
		return false
	}

	if runOnce.DiskMB > registry.availableDiskMB() {
		return false
	}

	return true
}

func (registry *TaskRegistry) availableMemoryMB() int {
	usedMemory := 0
	for _, r := range registry.RunOnces {
		usedMemory = usedMemory + r.MemoryMB
	}
	return registry.ExecutorMemoryMB - usedMemory
}

func (registry *TaskRegistry) availableDiskMB() int {
	usedDisk := 0
	for _, r := range registry.RunOnces {
		usedDisk = usedDisk + r.DiskMB
	}
	return registry.ExecutorDiskMB - usedDisk
}
