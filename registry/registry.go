package registry

import (
	"errors"
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models/executor_api"
	"github.com/nu7hatch/gouuid"
)

var ErrContainerNotFound = errors.New("container not found")
var ErrContainerNotReserved = errors.New("container not reserved")

var blankContainer = executor_api.Container{}

type Registry interface {
	CurrentCapacity() Capacity
	FindByGuid(guid string) (executor_api.Container, error)
	Reserve(executor_api.ContainerAllocationRequest) (executor_api.Container, error)
	Create(guid, containerHandle string) (executor_api.Container, error)
	Delete(guid string) error
}

type registry struct {
	executorGuid         string
	currentCapacity      *Capacity
	registeredContainers map[string]executor_api.Container
	containersMutex      *sync.RWMutex
}

func New(executorGuid string, capacity Capacity) Registry {
	return &registry{
		executorGuid:         executorGuid,
		currentCapacity:      &capacity,
		registeredContainers: make(map[string]executor_api.Container),
		containersMutex:      &sync.RWMutex{},
	}
}

func (r *registry) CurrentCapacity() Capacity {
	r.containersMutex.RLock()
	defer r.containersMutex.RUnlock()

	return *r.currentCapacity
}

func (r *registry) FindByGuid(guid string) (executor_api.Container, error) {
	r.containersMutex.RLock()
	defer r.containersMutex.RUnlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	return res, nil
}

func (r *registry) Reserve(req executor_api.ContainerAllocationRequest) (executor_api.Container, error) {
	guid, err := uuid.NewV4()
	if err != nil {
		return executor_api.Container{}, err
	}

	res := executor_api.Container{
		Guid:         guid.String(),
		ExecutorGuid: r.executorGuid,
		MemoryMB:     req.MemoryMB,
		DiskMB:       req.DiskMB,
		CpuPercent:   req.CpuPercent,
		State:        executor_api.StateReserved,
	}

	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	err = r.currentCapacity.alloc(res)
	if err != nil {
		return executor_api.Container{}, err
	}

	r.registeredContainers[res.Guid] = res

	return res, nil
}

func (r *registry) Create(guid, containerHandle string) (executor_api.Container, error) {
	r.containersMutex.Lock()
	defer r.containersMutex.Unlock()

	res, ok := r.registeredContainers[guid]
	if !ok {
		return blankContainer, ErrContainerNotFound
	}

	if res.State != executor_api.StateReserved {
		return blankContainer, ErrContainerNotReserved
	}

	res.State = executor_api.StateCreated
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
