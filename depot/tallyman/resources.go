package tallyman

import (
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type Tallyman struct {
	allocated    map[string]executor.Container
	timeProvider timeprovider.TimeProvider
	lock         sync.RWMutex
}

func NewTallyman(timeProvider timeprovider.TimeProvider) *Tallyman {
	return &Tallyman{
		allocated:    map[string]executor.Container{},
		timeProvider: timeProvider,
	}
}

func (t *Tallyman) List() []executor.Container {
	t.lock.RLock()
	defer t.lock.RUnlock()

	containers := make([]executor.Container, 0, len(t.allocated))

	for _, container := range t.allocated {
		containers = append(containers, container)
	}

	return containers
}

func (t *Tallyman) Lookup(guid string) (executor.Container, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.lookup(guid)
}

func (t *Tallyman) Allocate(logger lager.Logger, container executor.Container) (executor.Container, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, err := t.lookup(container.Guid); err == nil {
		logger.Error("failed-allocating-container", err)
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}
	logger.Debug("allocating-container", lager.Data{"container": container})

	container.State = executor.StateReserved
	container.AllocatedAt = t.timeProvider.Now().UnixNano()
	t.allocated[container.Guid] = container

	return container, nil
}

func (t *Tallyman) Initialize(logger lager.Logger, guid string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	container, err := t.lookup(guid)
	if err != nil {
		logger.Error("failed-initializing-container", err)
		return err
	}
	logger.Debug("initializing-container", lager.Data{"guid": guid})

	if container.State != executor.StateReserved {
		logger.Error(
			"failed-initializing-container",
			executor.ErrInvalidTransition,
			lager.Data{
				"currentState":  container.State,
				"expectedState": executor.StateReserved,
			},
		)
		return executor.ErrInvalidTransition
	}

	container.State = executor.StateInitializing
	t.allocated[guid] = container

	return nil
}

func (t *Tallyman) Fail(logger lager.Logger, guid string, reason string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	container, err := t.lookup(guid)
	if err != nil {
		logger.Error("failed-completing-container", err)
		return err
	}

	if container.State != executor.StateInitializing {
		logger.Error(
			"failed-completing-container",
			executor.ErrInvalidTransition,
			lager.Data{
				"currentState":  container.State,
				"expectedState": executor.StateInitializing,
			},
		)
		return executor.ErrInvalidTransition
	}
	logger.Debug("marking-container-completed-with-failure-reason", lager.Data{"guid": guid})

	container.State = executor.StateCompleted
	container.RunResult = executor.ContainerRunResult{
		Failed:        true,
		FailureReason: reason,
	}
	t.allocated[guid] = container

	return nil
}

func (t *Tallyman) Deallocate(logger lager.Logger, guid string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	_, err := t.lookup(guid)
	if err != nil {
		logger.Error("failed-deallocating-container", err)
		return err
	}
	logger.Debug("deallocating-container", lager.Data{"guid": guid})
	// Do we need to perform any state validations here?
	delete(t.allocated, guid)
	return nil
}

func (t *Tallyman) lookup(guid string) (executor.Container, error) {
	container, found := t.allocated[guid]
	if !found {
		return executor.Container{}, executor.ErrContainerNotFound
	}

	return container, nil
}

func (t *Tallyman) RegistryPruner(logger lager.Logger, expirationTime time.Duration) ifrit.Runner {
	logger = logger.Session("allocation-store-pruner")

	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		ticker := t.timeProvider.NewTicker(expirationTime / 2)
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

				t.lock.Lock()

				for guid, container := range t.allocated {
					if container.State != executor.StateReserved {
						// only prune reserved containers
						continue
					}

					lifespan := t.timeProvider.Now().Sub(time.Unix(0, container.AllocatedAt))

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
					delete(t.allocated, guid)
				}

				t.lock.Unlock()
			}
		}

		return nil
	})
}
