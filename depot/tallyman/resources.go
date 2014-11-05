package tallyman

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor"
)

type Tallyman struct {
	allocated   map[string]executor.Container
	initialized map[string]executor.Container

	lock sync.RWMutex
}

func NewTallyman() *Tallyman {
	return &Tallyman{
		allocated:   map[string]executor.Container{},
		initialized: map[string]executor.Container{},
	}
}

func (t *Tallyman) Allocations() []executor.Container {
	t.lock.RLock()

	containers := make([]executor.Container, 0, len(t.allocated)+len(t.initialized))

	for _, container := range t.allocated {
		containers = append(containers, container)
	}

	for _, container := range t.initialized {
		containers = append(containers, container)
	}

	t.lock.RUnlock()

	return containers
}

func (t *Tallyman) Allocate(container executor.Container) {
	t.lock.Lock()
	t.allocated[container.Guid] = container
	t.lock.Unlock()
}

func (t *Tallyman) Deallocate(guid string) {
	t.lock.Lock()
	delete(t.allocated, guid)
	t.lock.Unlock()
}

func (t *Tallyman) Initialize(container executor.Container) {
	t.lock.Lock()
	delete(t.allocated, container.Guid)
	t.initialized[container.Guid] = container
	t.lock.Unlock()
}

func (t *Tallyman) Deinitialize(guid string) {
	t.lock.Lock()
	delete(t.initialized, guid)
	t.lock.Unlock()
}

func (t *Tallyman) SyncInitialized(containers []executor.Container) {
	t.lock.Lock()

	t.initialized = map[string]executor.Container{}

	for _, container := range containers {
		delete(t.allocated, container.Guid)
		t.initialized[container.Guid] = container
	}

	t.lock.Unlock()
}
