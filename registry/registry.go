package registry

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
)

var ErrContainerAlreadyExists = errors.New("container already exists")
var ErrContainerNotFound = errors.New("container not found")
var ErrContainerNotReserved = errors.New("container not reserved")

var blankContainer = api.Container{}

type Registry interface {
	CurrentCapacity() Capacity
	TotalCapacity() Capacity
	FindByGuid(guid string) (api.Container, error)
	GetAllContainers() []api.Container
	Reserve(guid string, req api.ContainerAllocationRequest) (api.Container, error)
	Create(guid, containerHandle string) (api.Container, error)
	Delete(guid string) error
}

type registry struct {
	executorGuid         string
	totalCapacity        Capacity
	currentCapacity      *Capacity
	registeredContainers map[string]api.Container
	containersMutex      *sync.RWMutex
}

func New(executorGuid string, capacity Capacity) Registry {
	return &registry{
		executorGuid:         executorGuid,
		totalCapacity:        capacity,
		currentCapacity:      &capacity,
		registeredContainers: make(map[string]api.Container),
		containersMutex:      &sync.RWMutex{},
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
		Guid:         guid,
		ExecutorGuid: r.executorGuid,
		MemoryMB:     req.MemoryMB,
		DiskMB:       req.DiskMB,
		CpuPercent:   req.CpuPercent,
		Ports:        req.Ports,
		State:        api.StateReserved,
		Log:          req.Log,
		Metadata:     req.Metadata,
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

func (r *registry) Create(guid, containerHandle string) (api.Container, error) {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	if res.State != api.StateReserved {
		return blankContainer, ErrContainerNotReserved
	}

	res.State = api.StateCreated
	res.ContainerHandle = containerHandle
	r.registeredContainers[guid] = res
	return res, nil
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
