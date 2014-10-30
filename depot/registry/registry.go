package registry

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/tedsuo/ifrit"
)

var ErrContainerAlreadyExists = errors.New("container already exists")
var ErrContainerNotFound = errors.New("container not found")
var ErrContainerNotInitialized = errors.New("container not initialized")

var blankContainer = executor.Container{}

type Registry interface {
	CurrentCapacity() Capacity
	TotalCapacity() Capacity
	Reserve(container executor.Container) error
	Start(guid string, process ifrit.Process)
	Delete(container executor.Container)
}

type registry struct {
	totalCapacity   Capacity
	currentCapacity *Capacity
	capacityMutex   *sync.RWMutex

	processesMutex *sync.RWMutex
	processes      map[string]ifrit.Process
}

func New(capacity Capacity, timeProvider timeprovider.TimeProvider) Registry {
	return &registry{
		totalCapacity:   capacity,
		currentCapacity: &capacity,
		capacityMutex:   &sync.RWMutex{},

		processes:      make(map[string]ifrit.Process),
		processesMutex: &sync.RWMutex{},
	}
}

func (r *registry) TotalCapacity() Capacity {
	return r.totalCapacity
}

func (r *registry) CurrentCapacity() Capacity {
	r.capacityMutex.RLock()
	defer r.capacityMutex.RUnlock()
	return *r.currentCapacity
}

func (r *registry) Reserve(container executor.Container) error {
	r.capacityMutex.Lock()
	defer r.capacityMutex.Unlock()
	return r.currentCapacity.alloc(container)
}

func (r *registry) Start(guid string, process ifrit.Process) {
	r.processesMutex.Lock()
	defer r.processesMutex.Unlock()
	r.processes[guid] = process
}

func (r *registry) Delete(container executor.Container) {
	r.capacityMutex.Lock()
	defer r.capacityMutex.Unlock()

	delete(r.processes, container.Guid)
	r.currentCapacity.free(container)
}
