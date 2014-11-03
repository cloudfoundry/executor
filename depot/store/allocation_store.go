package store

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry/gunk/timeprovider"
)

type AllocationStore struct {
	timeProvider timeprovider.TimeProvider

	containers map[string]executor.Container
	mutex      sync.RWMutex
}

func NewAllocationStore(
	timeProvider timeprovider.TimeProvider,
	expirationTime time.Duration,
) *AllocationStore {
	store := &AllocationStore{
		timeProvider: timeProvider,
		containers:   make(map[string]executor.Container),
	}

	go store.reapExpiredAllocations(expirationTime)

	return store
}

func (store *AllocationStore) Lookup(guid string) (executor.Container, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	container, ok := store.containers[guid]
	if ok {
		return container, nil
	} else {
		return executor.Container{}, ErrContainerNotFound
	}
}

func (store *AllocationStore) List(tags executor.Tags) ([]executor.Container, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	result := make([]executor.Container, 0, len(store.containers))
	for _, container := range store.containers {
		if tagsMatch(tags, container.Tags) {
			result = append(result, container)
		}
	}

	return result, nil
}

func tagsMatch(needles, haystack executor.Tags) bool {
	for k, v := range needles {
		if haystack[k] != v {
			return false
		}
	}

	return true
}

func (store *AllocationStore) Create(container executor.Container) (executor.Container, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if _, ok := store.containers[container.Guid]; ok {
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	container.State = executor.StateReserved
	container.AllocatedAt = store.timeProvider.Time().UnixNano()

	store.containers[container.Guid] = container

	return container, nil
}

func (store *AllocationStore) Destroy(guid string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if _, found := store.containers[guid]; found {
		delete(store.containers, guid)
	} else {
		return ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) ConsumedResources() executor.ExecutorResources {
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	resources := executor.ExecutorResources{}

	for _, container := range store.containers {
		resources.Containers++
		resources.MemoryMB += container.MemoryMB
		resources.DiskMB += container.DiskMB
	}

	return resources
}

func (store *AllocationStore) Complete(guid string, result executor.ContainerRunResult) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if container, found := store.containers[guid]; found {
		container.State = executor.StateCompleted
		container.RunResult = result
		store.containers[guid] = container
	} else {
		return ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) StartInitializing(guid string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if container, found := store.containers[guid]; found {
		container.State = executor.StateInitializing
		store.containers[guid] = container
	} else {
		return ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) reapExpiredAllocations(expirationTime time.Duration) {
	// / 2 is not very significant
	ticker := time.NewTicker(expirationTime / 2)

	for {
		<-ticker.C

		expiredAllocations := []string{}

		store.mutex.RLock()

		for guid, container := range store.containers {
			if container.State != executor.StateReserved {
				// only prune reserved containers
				continue
			}

			lifespan := store.timeProvider.Time().Sub(time.Unix(0, container.AllocatedAt))

			if lifespan >= expirationTime {
				expiredAllocations = append(expiredAllocations, guid)
			}
		}

		store.mutex.RUnlock()

		if len(expiredAllocations) == 0 {
			continue
		}

		store.mutex.Lock()

		for _, guid := range expiredAllocations {
			delete(store.containers, guid)
		}

		store.mutex.Unlock()
	}
}
