package allocationstore

import (
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type AllocationStore struct {
	allocated    map[string]executor.Container
	clock        clock.Clock
	eventEmitter EventEmitter
	lock         sync.RWMutex
}

func NewAllocationStore(clock clock.Clock, eventEmitter EventEmitter) *AllocationStore {
	return &AllocationStore{
		allocated:    map[string]executor.Container{},
		clock:        clock,
		eventEmitter: eventEmitter,
	}
}

func (a *AllocationStore) List() []executor.Container {
	a.lock.RLock()
	defer a.lock.RUnlock()

	containers := make([]executor.Container, 0, len(a.allocated))

	for key := range a.allocated {
		containers = append(containers, a.allocated[key].Copy())
	}

	return containers
}

func (a *AllocationStore) Lookup(guid string) (executor.Container, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.lookup(guid)
}

func (a *AllocationStore) Allocate(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if _, err := a.lookup(req.Guid); err == nil {
		logger.Error("failed-allocating-container", err)
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	logger.Debug("allocating-container", lager.Data{"allocation-request": req})

	a.allocated[req.Guid] = executor.NewReservedContainerFromAllocationRequest(req,a.clock.Now().UnixNano())

	a.eventEmitter.Emit(executor.NewContainerReservedEvent(a.allocated[req.Guid]))

	return a.allocated[req.Guid].Copy(), nil
}

func (a *AllocationStore) Initialize(logger lager.Logger, req *executor.RunRequest) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	container, err := a.lookup(req.Guid)
	if err != nil {
		logger.Error("failed-initializing-container", err)
		return err
	}
	logger.Debug("initializing-container", lager.Data{"guid": req.Guid})

	if container.State != executor.StateReserved {
		logger.Error(
			"failed-initializing-container",
			executor.ErrInvalidTransition,
			lager.Data{
				"current_state":  container.State,
				"expected_state": executor.StateReserved,
			},
		)
		return executor.ErrInvalidTransition
	}

	container.State = executor.StateInitializing
	container.RunInfo = req.RunInfo
	container.Tags.Add(req.Tags)
	a.allocated[container.Guid] = container

	return nil
}

func (a *AllocationStore) Fail(logger lager.Logger, guid string, reason string) (executor.Container, error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	container, err := a.lookup(guid)
	if err != nil {
		logger.Error("failed-completing-container", err)
		return executor.Container{}, err
	}

	if container.State == executor.StateCompleted {
		logger.Error(
			"failed-completing-container",
			executor.ErrInvalidTransition,
			lager.Data{
				"current_state":  container.State,
				"expected_state": executor.StateInitializing,
			},
		)
		return executor.Container{}, executor.ErrInvalidTransition
	}

	logger.Debug("marking-container-completed-with-failure-reason", lager.Data{
		"guid":           guid,
		"failure_reason": reason,
	})

	container.State = executor.StateCompleted
	container.RunResult = executor.ContainerRunResult{
		Failed:        true,
		FailureReason: reason,
	}
	a.allocated[guid] = container

	a.eventEmitter.Emit(executor.NewContainerCompleteEvent(container))

	return container, nil
}

func (a *AllocationStore) Deallocate(logger lager.Logger, guid string) bool {
	a.lock.Lock()
	defer a.lock.Unlock()

	_, allocated := a.allocated[guid]
	if !allocated {
		logger.Debug("container-already-deallocated", lager.Data{"guid": guid})
		return false
	}

	logger.Debug("deallocating-container", lager.Data{"guid": guid})

	delete(a.allocated, guid)

	return true
}

func (a *AllocationStore) lookup(guid string) (executor.Container, error) {
	container, found := a.allocated[guid]
	if !found {
		return executor.Container{}, executor.ErrContainerNotFound
	}

	return container, nil
}

func (a *AllocationStore) RegistryPruner(logger lager.Logger, expirationTime time.Duration) ifrit.Runner {
	logger = logger.Session("allocation-store-pruner")

	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		ticker := a.clock.NewTicker(expirationTime / 2)
		defer ticker.Stop()

		close(ready)

		for {
			select {
			case <-signals:
				logger.Info("exiting-pruning-loop")
				return nil

			case <-ticker.C():
				logger.Debug("checking-for-expired-containers")
				expiredAllocations := []string{}

				a.lock.Lock()

				for guid, container := range a.allocated {
					if container.State != executor.StateReserved {
						// only prune reserved containers
						continue
					}

					lifespan := a.clock.Now().Sub(time.Unix(0, container.AllocatedAt))

					if lifespan >= expirationTime {
						logger.Info("reserved-container-expired", lager.Data{"guid": guid, "lifespan": lifespan})
						expiredAllocations = append(expiredAllocations, guid)
					}
				}

				if len(expiredAllocations) > 0 {
					logger.Info("reaping-expired-allocations", lager.Data{"num-reaped": len(expiredAllocations)})
				} else {
					logger.Info("no-expired-allocations-found")
				}

				for _, guid := range expiredAllocations {
					logger.Info("deleting-expired-container", lager.Data{"guid": guid})
					delete(a.allocated, guid)
				}

				a.lock.Unlock()
			}
		}

		return nil
	})
}
