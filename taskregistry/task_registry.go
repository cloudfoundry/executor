package taskregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var ErrorRegistrySnapshotDoesNotExist = errors.New("Registry snapshot does not exist")
var ErrorRegistrySnapshotHasInvalidJSON = errors.New("Registry snapshot has invalid JSON")
var ErrorNotEnoughMemoryWhenLoadingSnapshot = errors.New("Insufficient memory when loading snapshot")
var ErrorNotEnoughDiskWhenLoadingSnapshot = errors.New("Insufficient disk when loading snapshot")

type TaskRegistryInterface interface {
	AddRunOnce(runOnce models.RunOnce) error
	RemoveRunOnce(runOnce models.RunOnce)
	WriteToDisk() error
}

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	RunOnces         map[string]models.RunOnce
	lock             *sync.Mutex

	stack    string
	fileName string
}

func NewTaskRegistry(stack string, fileName string, memoryMB int, diskMB int) *TaskRegistry {
	return &TaskRegistry{
		ExecutorMemoryMB: memoryMB,
		ExecutorDiskMB:   diskMB,
		RunOnces:         make(map[string]models.RunOnce),

		lock: &sync.Mutex{},

		stack:    stack,
		fileName: fileName,
	}
}

func LoadTaskRegistryFromDisk(stack string, filename string, memoryMB int, diskMB int) (*TaskRegistry, error) {
	taskRegistry := NewTaskRegistry(stack, filename, memoryMB, diskMB)
	err := taskRegistry.hydrateFromDisk()
	if err != nil {
		return nil, err
	}
	return taskRegistry, nil
}

func (registry *TaskRegistry) AddRunOnce(runOnce models.RunOnce) error {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	if !registry.hasCapacityForRunOnce(runOnce) {
		return fmt.Errorf("insufficient resources to claim run once: Desired %d (memory) %d (disk).  Have %d (memory) %d (disk).", runOnce.MemoryMB, runOnce.DiskMB, registry.availableMemoryMB(), registry.availableDiskMB())
	}

	if runOnce.Stack != "" && runOnce.Stack != registry.stack {
		return fmt.Errorf(
			"run once has incompatible stack: have %s, want %s",
			runOnce.Stack,
			registry.stack,
		)
	}

	registry.RunOnces[runOnce.Guid] = runOnce
	return nil
}

func (registry *TaskRegistry) RemoveRunOnce(runOnce models.RunOnce) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	delete(registry.RunOnces, runOnce.Guid)
}

func (registry *TaskRegistry) WriteToDisk() error {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(registry.fileName, data, os.ModePerm)
}

func (registry *TaskRegistry) hydrateFromDisk() error {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	var loadedTaskRegistry *TaskRegistry
	bytes, err := ioutil.ReadFile(registry.fileName)
	if err != nil {
		return ErrorRegistrySnapshotDoesNotExist
	}
	err = json.Unmarshal(bytes, &loadedTaskRegistry)
	if err != nil {
		return ErrorRegistrySnapshotHasInvalidJSON
	}

	registry.RunOnces = loadedTaskRegistry.RunOnces

	if registry.availableMemoryMB() < 0 {
		return ErrorNotEnoughMemoryWhenLoadingSnapshot
	}

	if registry.availableDiskMB() < 0 {
		return ErrorNotEnoughDiskWhenLoadingSnapshot
	}

	return nil
}

func (registry *TaskRegistry) hasCapacityForRunOnce(runOnce models.RunOnce) bool {
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
