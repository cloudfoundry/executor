package store

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/pivotal-golang/lager"
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
		return executor.Container{}, executor.ErrContainerNotFound
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

func (store *AllocationStore) Create(logger lager.Logger, container executor.Container) (executor.Container, error) {
	store.lock.Lock()
	defer store.lock.Unlock()

	if _, ok := store.containers[container.Guid]; ok {
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	container.State = executor.StateReserved
	container.AllocatedAt = store.timeProvider.Now().UnixNano()

	store.tracker.Allocate(container)

	store.containers[container.Guid] = container

	store.emitter.EmitEvent(executor.NewContainerReservedEvent(container))

	return container, nil
}

func (store *AllocationStore) Destroy(logger lager.Logger, guid string) error {
	store.lock.Lock()
	defer store.lock.Unlock()

	if _, found := store.containers[guid]; found {
		delete(store.containers, guid)
		store.tracker.Deallocate(guid)
	} else {
		return executor.ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) Complete(guid string, result executor.ContainerRunResult) error {
	store.lock.Lock()
	defer store.lock.Unlock()

	if container, found := store.containers[guid]; found {
		if container.State == executor.StateCompleted || container.State == executor.StateReserved {
			return executor.ErrInvalidTransition
		}
		container.State = executor.StateCompleted
		container.RunResult = result

		store.emitter.EmitEvent(executor.NewContainerCompleteEvent(container))

		store.containers[guid] = container
	} else {
		return executor.ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) StartInitializing(guid string) error {
	store.lock.Lock()
	defer store.lock.Unlock()

	if container, found := store.containers[guid]; found {
		if container.State != executor.StateReserved {
			return executor.ErrInvalidTransition
		}
		container.State = executor.StateInitializing
		store.containers[guid] = container
	} else {
		return executor.ErrContainerNotFound
	}

	return nil
}

func (store *AllocationStore) reapExpiredAllocations(expirationTime time.Duration) {
	// / 2 is not very significant
	ticker := time.NewTicker(expirationTime / 2)

	for {
		<-ticker.C

		expiredAllocations := []string{}

		store.lock.Lock()

		for guid, container := range store.containers {
			if container.State != executor.StateReserved {
				// only prune reserved containers
				continue
			}

			lifespan := store.timeProvider.Now().Sub(time.Unix(0, container.AllocatedAt))

			if lifespan >= expirationTime {
				expiredAllocations = append(expiredAllocations, guid)
			}
		}

		if len(expiredAllocations) == 0 {
			store.lock.Unlock()
			continue
		}

		for _, guid := range expiredAllocations {
			store.tracker.Deallocate(guid)
			delete(store.containers, guid)
		}

		store.lock.Unlock()
	}
}
