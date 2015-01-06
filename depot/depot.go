package depot

import (
	"io"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	"github.com/pivotal-golang/lager"
)

const ContainerInitializationFailedMessage = "failed to initialize container"

type AllocationsTracker interface {
	Allocations() []executor.Container
}

type client struct {
	*clientProvider
	logger lager.Logger
}

type clientProvider struct {
	totalCapacity   executor.ExecutorResources
	gardenStore     GardenStore
	allocationStore AllocationStore
	tracker         AllocationsTracker
	eventHub        event.Hub

	resourcesL sync.Mutex
}

type Store interface {
	Create(lager.Logger, executor.Container) (executor.Container, error)
	Lookup(guid string) (executor.Container, error)
	List(executor.Tags) ([]executor.Container, error)
	Destroy(logger lager.Logger, guid string) error
}

type AllocationStore interface {
	Store

	StartInitializing(guid string) error
	Complete(guid string, result executor.ContainerRunResult) error
}

type GardenStore interface {
	Store

	Ping() error
	Run(lager.Logger, executor.Container) error
	Stop(logger lager.Logger, guid string) error
	GetFiles(guid, sourcePath string) (io.ReadCloser, error)
}

func NewClientProvider(
	totalCapacity executor.ExecutorResources,
	gardenStore GardenStore,
	allocationStore AllocationStore,
	tracker AllocationsTracker,
	eventHub event.Hub,
) executor.ClientProvider {
	return &clientProvider{
		totalCapacity:   totalCapacity,
		gardenStore:     gardenStore,
		allocationStore: allocationStore,
		tracker:         tracker,
		eventHub:        eventHub,
	}
}

func (provider *clientProvider) WithLogger(logger lager.Logger) executor.Client {
	return &client{
		provider,
		logger.Session("depot-client"),
	}
}

func (c *client) AllocateContainer(executorContainer executor.Container) (executor.Container, error) {
	if executorContainer.CPUWeight > 100 || executorContainer.CPUWeight < 0 {
		return executor.Container{}, executor.ErrLimitsInvalid
	} else if executorContainer.CPUWeight == 0 {
		executorContainer.CPUWeight = 100
	}

	if executorContainer.Guid == "" {
		return executor.Container{}, executor.ErrGuidNotSpecified
	}

	logger := c.logger.Session("allocate", lager.Data{
		"guid": executorContainer.Guid,
	})

	c.resourcesL.Lock()
	defer c.resourcesL.Unlock()

	if !c.hasSpace(executorContainer) {
		logger.Info("full")
		return executor.Container{}, executor.ErrInsufficientResourcesAvailable
	}

	createdContainer, err := c.allocationStore.Create(logger, executorContainer)
	if err != nil {
		logger.Error("container-allocation-failed", err)
		return executor.Container{}, err
	}

	return createdContainer, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	container, err := c.allocationStore.Lookup(guid)
	if err != nil {
		container, err = c.gardenStore.Lookup(guid)
		if err != nil {
			logger.Error("container-not-found", err)
			return executor.Container{}, executor.ErrContainerNotFound
		}
	}

	return container, nil
}

func (c *client) RunContainer(guid string) error {
	logger := c.logger.Session("run", lager.Data{
		"guid": guid,
	})

	err := c.allocationStore.StartInitializing(guid)
	if err != nil {
		logger.Error("failed-to-initialize-container", err)
		return executor.ErrContainerNotFound
	}

	go func() {
		container, err := c.allocationStore.Lookup(guid)
		if err != nil {
			logger.Error("failed-to-find-container", err)
			return
		}

		container, err = c.gardenStore.Create(logger, container)
		if err != nil {
			logger.Error("failed-to-create-container", err)

			c.allocationStore.Complete(guid, executor.ContainerRunResult{
				Failed:        true,
				FailureReason: ContainerInitializationFailedMessage,
			})

			return
		}

		err = c.allocationStore.Destroy(logger, guid)
		if err == store.ErrContainerNotFound {
			logger.Debug("container-deleted-while-initilizing")

			err := c.gardenStore.Destroy(logger, container.Guid)
			if err != nil {
				logger.Error("failed-to-destroy-newly-initialized-container", err)
			}

			logger.Info("destroyed-newly-initialized-container")

			return
		} else if err != nil {
			logger.Error("failed-to-remove-allocated-container-for-some-reason", err)
			return
		}

		err = c.gardenStore.Run(logger, container)
		if err != nil {
			logger.Error("failed-to-run-newly-created-container", err)
		}
	}()

	return nil
}

func (c *client) ListContainers(tags executor.Tags) ([]executor.Container, error) {
	containersByHandle := make(map[string]executor.Container)

	// order here is important; listing containers from garden takes time, and in
	// that time a container may transition from allocation store to garden
	// store.
	//
	// in this case, if the garden store were fetched first, the container would
	// not be listed anywhere. this ordering guarantees that it will at least
	// show up allocated.

	allocatedContainers, err := c.allocationStore.List(tags)
	if err != nil {
		return nil, err
	}

	gardenContainers, err := c.gardenStore.List(tags)
	if err != nil {
		return nil, err
	}

	//the ordering here is important.  we want allocated containers to override
	//garden containers as a member of latter is uninitialized until its
	//counterpart in the former is gone and we don't want to publicize containers that
	//are in a half-baked state
	for _, gardenContainer := range gardenContainers {
		containersByHandle[gardenContainer.Guid] = gardenContainer
	}

	for _, gardenContainer := range allocatedContainers {
		containersByHandle[gardenContainer.Guid] = gardenContainer
	}

	containers := make([]executor.Container, 0, len(containersByHandle))
	for _, container := range containersByHandle {
		containers = append(containers, container)
	}

	return containers, nil
}

func (c *client) StopContainer(guid string) error {
	logger := c.logger.Session("stop", lager.Data{
		"guid": guid,
	})

	logger.Info("stopping-container")
	return c.gardenStore.Stop(logger, guid)
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete", lager.Data{
		"guid": guid,
	})

	if err := c.allocationStore.Destroy(logger, guid); err == nil {
		logger.Info("deleted-allocation")
		return nil
	}

	container, err := c.gardenStore.Lookup(guid)
	if err != nil {
		logger.Error("container-not-found", err)
		return err
	}

	if container.State != executor.StateCompleted {
		logger.Error("container-not-completed", err)
		return executor.ErrContainerNotCompleted
	}

	err = c.gardenStore.Destroy(logger, guid)
	if err != nil {
		logger.Error("failed-to-destroy", err)
		return executor.ErrContainerNotFound
	}

	return nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	remainingResources, err := c.TotalResources()
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	for _, allocation := range c.tracker.Allocations() {
		remainingResources.Containers--
		remainingResources.DiskMB -= allocation.DiskMB
		remainingResources.MemoryMB -= allocation.MemoryMB
	}

	return remainingResources, nil
}

func (c *client) Ping() error {
	return c.gardenStore.Ping()
}

func (c *client) TotalResources() (executor.ExecutorResources, error) {
	totalCapacity := c.totalCapacity

	return executor.ExecutorResources{
		MemoryMB:   totalCapacity.MemoryMB,
		DiskMB:     totalCapacity.DiskMB,
		Containers: totalCapacity.Containers,
	}, nil
}

func (c *client) GetFiles(guid, sourcePath string) (io.ReadCloser, error) {
	return c.gardenStore.GetFiles(guid, sourcePath)
}

func (c *client) SubscribeToEvents() (<-chan executor.Event, error) {
	return c.eventHub.Subscribe(), nil
}

func (c *client) hasSpace(container executor.Container) bool {
	remainingResources, err := c.RemainingResources()
	if err != nil {
		panic("welp")
	}

	if remainingResources.MemoryMB < container.MemoryMB {
		return false
	}

	if remainingResources.DiskMB < container.DiskMB {
		return false
	}

	if remainingResources.Containers < 1 {
		return false
	}

	return true
}
