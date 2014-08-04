package registry

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/tedsuo/ifrit"
)

var ErrContainerAlreadyExists = errors.New("container already exists")
var ErrContainerNotFound = errors.New("container not found")
var ErrContainerNotInitialized = errors.New("container not initialized")

var blankContainer = api.Container{}

type Registry interface {
	CurrentCapacity() Capacity
	TotalCapacity() Capacity
	FindByGuid(guid string) (api.Container, error)
	GetAllContainers() []api.Container
	Reserve(guid string, req api.ContainerAllocationRequest) (api.Container, error)
	Initialize(guid string) (api.Container, error)
	Create(guid, containerHandle string, req api.ContainerInitializationRequest) (api.Container, error)
	Start(guid string, process ifrit.Process) error
	Complete(guid string, result api.ContainerRunResult) error
	Delete(guid string) error
}

type registry struct {
	totalCapacity        Capacity
	currentCapacity      *Capacity
	timeProvider         timeprovider.TimeProvider
	registeredContainers map[string]api.Container
	containersMutex      *sync.RWMutex
}

func New(capacity Capacity, timeProvider timeprovider.TimeProvider) Registry {
	return &registry{
		totalCapacity:        capacity,
		currentCapacity:      &capacity,
		registeredContainers: make(map[string]api.Container),
		containersMutex:      &sync.RWMutex{},
		timeProvider:         timeProvider,
	}
}

func (r *registry) TotalCapacity() Capacity {
	return r.totalCapacity
}

func (r *registry) CurrentCapacity() Capacity {
	r.containersMutex.RLock()
	defer r.containersMutex.RUnlock()

	return *r.currentCapacity
}

func (r *registry) GetAllContainers() []api.Container {
	r.containersMutex.RLock()
	defer r.containersMutex.RUnlock()

	containers := []api.Container{}
	for _, container := range r.registeredContainers {
		containers = append(containers, container)
	}

	return containers
}

func (r *registry) FindByGuid(guid string) (api.Container, error) {
	r.containersMutex.RLock()
	defer r.containersMutex.RUnlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	return res, nil
}

func (r *registry) Reserve(guid string, req api.ContainerAllocationRequest) (api.Container, error) {
	res := api.Container{
		Guid:        guid,
		MemoryMB:    req.MemoryMB,
		DiskMB:      req.DiskMB,
		State:       api.StateReserved,
		AllocatedAt: r.timeProvider.Time().UnixNano(),
	}

	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	_, ok := r.registeredContainers[guid]
	if ok {
		return api.Container{}, ErrContainerAlreadyExists
	}

	err := r.currentCapacity.alloc(res)
	if err != nil {
		return api.Container{}, err
	}

	r.registeredContainers[res.Guid] = res

	return res, nil
}

func (r *registry) Initialize(guid string) (api.Container, error) {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	if res.State != api.StateReserved {
		return blankContainer, ErrContainerNotInitialized
	}

	res.State = api.StateInitializing

	r.registeredContainers[guid] = res
	return res, nil
}

func (r *registry) Create(guid, containerHandle string, req api.ContainerInitializationRequest) (api.Container, error) {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	if res.State != api.StateInitializing {
		return blankContainer, ErrContainerNotInitialized
	}

	res.State = api.StateCreated
	res.ContainerHandle = containerHandle
	res.CpuPercent = req.CpuPercent
	res.Ports = req.Ports
	res.Log = req.Log
	res.RootFSPath = req.RootFSPath

	r.registeredContainers[guid] = res
	return res, nil
}

func (r *registry) Start(guid string, process ifrit.Process) error {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return ErrContainerNotFound
	}

	res.Process = process

	r.registeredContainers[guid] = res
	return nil
}

func (r *registry) Complete(guid string, result api.ContainerRunResult) error {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return ErrContainerNotFound
	}

	res.State = api.StateCompleted
	res.RunResult = result

	r.registeredContainers[guid] = res
	return nil
}

func (r *registry) Delete(guid string) error {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return ErrContainerNotFound
	}

	r.currentCapacity.free(res)
	delete(r.registeredContainers, guid)

	return nil
}
