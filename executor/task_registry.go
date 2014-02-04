package executor

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

var ErrorNotEnoughMemoryWhenLoadingSnapshot = errors.New("Insufficient memory when loading snapshot")
var ErrorNotEnoughDiskWhenLoadingSnapshot = errors.New("Insufficient disk when loading snapshot")

type TaskRegistry struct {
	ExecutorMemoryMB int
	ExecutorDiskMB   int
	RunOnces         map[string]models.RunOnce
	lock             *sync.Mutex
}

func NewTaskRegistry(memoryMB int, diskMB int) *TaskRegistry {
	return &TaskRegistry{
		ExecutorMemoryMB: memoryMB,
		ExecutorDiskMB:   diskMB,
		RunOnces:         make(map[string]models.RunOnce),
		lock:             &sync.Mutex{},
	}
}

func LoadTaskRegistryFromDisk(memoryMB int, diskMB int) (*TaskRegistry, error) {
	taskRegistry := NewTaskRegistry(memoryMB, diskMB)
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

func (registry *TaskRegistry) WriteToDisk() {
	fo, err := os.Create("saved_registry")
	if err != nil {
		//TODO: log and return the error, don't panic
		panic(err)
	}
	// close fo on exit and check for its returned error
	defer func() {
		if err := fo.Close(); err != nil {
			//TODO: log and return the error, don't panic
			panic(err)
		}
	}()

	json.NewEncoder(fo).Encode(registry)
}

func (registry *TaskRegistry) hydrateFromDisk() error {
	var loadedTaskRegistry *TaskRegistry
	bytes, err := ioutil.ReadFile("saved_registry")
	if err != nil {
		return err
	}
	err = json.Unmarshal(bytes, &loadedTaskRegistry)
	if err != nil {
		return err
	}

	registry.RunOnces = loadedTaskRegistry.RunOnces

	if registry.AvailableMemoryMB() < 0 {
		//TODO: log
		return ErrorNotEnoughMemoryWhenLoadingSnapshot
	}

	if registry.AvailableDiskMB() < 0 {
		//TODO: log
		return ErrorNotEnoughDiskWhenLoadingSnapshot
	}

	return nil
}
