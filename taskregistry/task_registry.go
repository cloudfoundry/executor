package taskregistry

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var ErrorRegistrySnapshotDoesNotExist = errors.New("Registry snapshot does not exist")
var ErrorRegistrySnapshotHasInvalidJSON = errors.New("Registry snapshot has invalid JSON")
var ErrorNotEnoughMemoryWhenLoadingSnapshot = errors.New("Insufficient memory when loading snapshot")
var ErrorNotEnoughDiskWhenLoadingSnapshot = errors.New("Insufficient disk when loading snapshot")

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	RunOnces         map[string]models.RunOnce
	lock             *sync.Mutex
	fileName         string
}

func NewTaskRegistry(fileName string, memoryMB int, diskMB int) *TaskRegistry {
	return &TaskRegistry{
		ExecutorMemoryMB: memoryMB,
		ExecutorDiskMB:   diskMB,
		RunOnces:         make(map[string]models.RunOnce),
		lock:             &sync.Mutex{},
		fileName:         fileName,
	}
}

func LoadTaskRegistryFromDisk(filename string, memoryMB int, diskMB int) (*TaskRegistry, error) {
	taskRegistry := NewTaskRegistry(filename, memoryMB, diskMB)
	err := taskRegistry.hydrateFromDisk()
	if err != nil {
		return nil, err
	}
	return taskRegistry, nil
}

func (registry *TaskRegistry) AddRunOnce(runOnce models.RunOnce) bool {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	if !registry.HasCapacityForRunOnce(runOnce) {
		return false
	}
	registry.RunOnces[runOnce.Guid] = runOnce
	return true
}

func (registry *TaskRegistry) RemoveRunOnce(runOnce models.RunOnce) {
	registry.lock.Lock()
	defer registry.lock.Unlock()

	delete(registry.RunOnces, runOnce.Guid)
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
	for _, r := range registry.RunOnces {
		usedMemory = usedMemory + r.MemoryMB
	}
	return registry.ExecutorMemoryMB - usedMemory
}

func (registry *TaskRegistry) AvailableDiskMB() int {
	usedDisk := 0
	for _, r := range registry.RunOnces {
		usedDisk = usedDisk + r.DiskMB
	}
	return registry.ExecutorDiskMB - usedDisk
}

func (registry *TaskRegistry) WriteToDisk() error {
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(registry.fileName, data, os.ModePerm)
}

func (registry *TaskRegistry) hydrateFromDisk() error {
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

	if registry.AvailableMemoryMB() < 0 {
		return ErrorNotEnoughMemoryWhenLoadingSnapshot
	}

	if registry.AvailableDiskMB() < 0 {
		return ErrorNotEnoughDiskWhenLoadingSnapshot
	}

	return nil
}
