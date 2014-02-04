package executor

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"sync"
)

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	runOnces         map[string]models.RunOnce
	lock             *sync.Mutex
}

func NewTaskRegistry(memoryMB int, diskMB int) *TaskRegistry {
	return &TaskRegistry{
		ExecutorMemoryMB: memoryMB,
		ExecutorDiskMB:   diskMB,
		runOnces:         make(map[string]models.RunOnce),
		lock:             &sync.Mutex{},
	}
}

func (registry *TaskRegistry) AddRunOnce(runOnce models.RunOnce) bool {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	if !registry.HasCapacityForRunOnce(runOnce) {
		return false
	}
	registry.runOnces[runOnce.Guid] = runOnce
	return true
}

func (registry *TaskRegistry) RemoveRunOnce(runOnce models.RunOnce) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	delete(registry.runOnces, runOnce.Guid)
}

func (registry *TaskRegistry) HasCapacityForRunOnce(runOnce models.RunOnce) bool {
	if runOnce.MemoryMB > registry.AvailableMemoryMB() {
		return false
	}

	if runOnce.DiskMB > registry.AvailableDiskMB() {
		return false
	}

	return true
}

func (registry *TaskRegistry) AvailableMemoryMB() int {
	usedMemory := 0
	for _, r := range registry.runOnces {
		usedMemory = usedMemory + r.MemoryMB
	}
	return registry.ExecutorMemoryMB - usedMemory
}

func (registry *TaskRegistry) AvailableDiskMB() int {
	usedDisk := 0
	for _, r := range registry.runOnces {
		usedDisk = usedDisk + r.DiskMB
	}
	return registry.ExecutorDiskMB - usedDisk
}

func (registry *TaskRegistry) RunOnces() map[string]models.RunOnce {
	return registry.runOnces
}
