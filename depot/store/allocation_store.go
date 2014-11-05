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
	lock       sync.RWMutex

	tracker AllocationTracker

	emitter EventEmitter
}

type AllocationTracker interface {
	Allocate(executor.Container)
	Deallocate(string)
}

func NewAllocationStore(
	timeProvider timeprovider.TimeProvider,
	expirationTime time.Duration,
	tracker AllocationTracker,
	emitter EventEmitter,
) *AllocationStore {
	store := &AllocationStore{
		timeProvider: timeProvider,

		containers: make(map[string]executor.Container),

		tracker: tracker,

		emitter: emitter,
	}

	go store.reapExpiredAllocations(expirationTime)

	return store
}

func (store *AllocationStore) Lookup(guid string) (executor.Container, error) {
	store.lock.RLock()
	defer store.lock.RUnlock()

	container, ok := store.containers[guid]
	if ok {
		return container, nil
	} else {
		return executor.Container{}, ErrContainerNotFound
	}
}

func (store *AllocationStore) List(tags executor.Tags) ([]executor.Container, error) {
	store.lock.RLock()
	defer store.lock.RUnlock()

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
	store.lock.Lock()
	defer store.lock.Unlock()

	if _, ok := store.containers[container.Guid]; ok {
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	container.State = executor.StateReserved
	container.AllocatedAt = store.timeProvider.Time().UnixNano()

	store.tracker.Allocate(container)

	store.containers[container.Guid] = container

	return container, nil
}

func (store *AllocationStore) Destroy(guid string) error {
	store.lock.Lock()
	defer store.lock.Unlock()

	if _, found := store.containers[guid]; found {
		delete(store.containers, guid)
		store.tracker.Deallocate(guid)
	} else {
		return ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) Complete(guid string, result executor.ContainerRunResult) error {
	store.lock.Lock()
	defer store.lock.Unlock()

	if container, found := store.containers[guid]; found {
		container.State = executor.StateCompleted
		container.RunResult = result

		store.emitter.EmitEvent(executor.ContainerCompleteEvent{
			Container: container,
		})

		store.containers[guid] = container
	} else {
		return ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) StartInitializing(guid string) error {
	store.lock.Lock()
	defer store.lock.Unlock()

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

		store.lock.RLock()

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

		store.lock.RUnlock()

		if len(expiredAllocations) == 0 {
			continue
		}

		store.lock.Lock()

		for _, guid := range expiredAllocations {
			store.tracker.Deallocate(guid)
			delete(store.containers, guid)
		}

		store.lock.Unlock()
	}
}
