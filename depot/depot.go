package depot

import (
	"fmt"
	"io"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/pivotal-golang/lager"
)

const ContainerInitializationFailedMessage = "failed to initialize container"

type client struct {
	*clientProvider
	logger lager.Logger
}

type clientProvider struct {
	totalCapacity   executor.ExecutorResources
	gardenStore     GardenStore
	allocationStore AllocationStore
	eventHub        event.Hub
	multiStoreLock  sync.Mutex
}

type AllocationStore interface {
	List() []executor.Container
	Lookup(guid string) (executor.Container, error)
	Allocate(logger lager.Logger, container executor.Container) (executor.Container, error)
	Initialize(logger lager.Logger, guid string) error
	Fail(logger lager.Logger, guid string, reason string) error
	Deallocate(logger lager.Logger, guid string) error
}

type GardenStore interface {
	Create(logger lager.Logger, container executor.Container) (executor.Container, error)
	Lookup(guid string) (executor.Container, error)
	List(tags executor.Tags) ([]executor.Container, error)
	Destroy(logger lager.Logger, guid string) error
	Ping() error
	Run(logger lager.Logger, container executor.Container) error
	Stop(logger lager.Logger, guid string) error
	GetFiles(guid, sourcePath string) (io.ReadCloser, error)
}

func NewClientProvider(
	totalCapacity executor.ExecutorResources,
	allocationStore AllocationStore,
	gardenStore GardenStore,
	eventHub event.Hub,
) executor.ClientProvider {
	return &clientProvider{
		totalCapacity:   totalCapacity,
		allocationStore: allocationStore,
		gardenStore:     gardenStore,
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

	c.multiStoreLock.Lock()
	defer c.multiStoreLock.Unlock()

	if !c.hasSpace(executorContainer) {
		logger.Info("full")
		return executor.Container{}, executor.ErrInsufficientResourcesAvailable
	}

	createdContainer, err := c.allocationStore.Allocate(logger, executorContainer)
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

	c.multiStoreLock.Lock()
	defer c.multiStoreLock.Unlock()

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

	err := c.allocationStore.Initialize(logger, guid)
	if err != nil {
		logger.Error("failed-to-initialize-container", err)
		return err
	}

	go func() {
		c.multiStoreLock.Lock()
		defer c.multiStoreLock.Unlock()

		container, err := c.allocationStore.Lookup(guid)
		if err != nil {
			logger.Error("failed-to-find-container", err)
			return
		}

		container, err = c.gardenStore.Create(logger, container)
		if err != nil {
			logger.Error("failed-to-create-container", err)

			c.allocationStore.Fail(logger, guid, ContainerInitializationFailedMessage)

			return
		}

		err = c.allocationStore.Deallocate(logger, guid)
		if err == executor.ErrContainerNotFound {
			logger.Debug("container-deleted-while-initilizing")

			err := c.gardenStore.Destroy(logger, guid)
			if err != nil {
				logger.Error("failed-to-destroy-newly-initialized-container", err)
			} else {
				logger.Info("destroyed-newly-initialized-container")
			}

			return
		} else if err != nil {
			logger.Error("failed-to-remove-allocated-container-for-unknown-reason", err)
			return
		}

		err = c.gardenStore.Run(logger, container)
		if err != nil {
			logger.Error("failed-to-run-newly-created-container", err)
		}
	}()

	return nil
}

func tagsMatch(needles, haystack executor.Tags) bool {
	for k, v := range needles {
		if haystack[k] != v {
			return false
		}
	}

	return true
}

func (c *client) ListContainers(tags executor.Tags) ([]executor.Container, error) {
	c.multiStoreLock.Lock()
	defer c.multiStoreLock.Unlock()

	// Order here is important; listing containers from garden takes time, and in
	// that time a container may transition from allocation store to garden
	// store.
	//
	// In this case, if the garden store were fetched first, the container would
	// not be listed anywhere. This ordering guarantees that it will at least
	// show up allocated.
	allAllocatedContainers := c.allocationStore.List()
	allocatedContainers := make([]executor.Container, 0, len(allAllocatedContainers))
	for _, container := range allAllocatedContainers {
		if tagsMatch(tags, container.Tags) {
			allocatedContainers = append(allocatedContainers, container)
		}
	}
	gardenContainers, err := c.gardenStore.List(tags)
	if err != nil {
		return nil, err
	}

	// Order here is important; we want allocated containers to override garden
	// containers as a member of latter is uninitialized until its counterpart
	// in the former is gone and we don't want to publicize containers that are
	// in a half-baked state.
	containersByGuid := map[string]executor.Container{}
	for _, gardenContainer := range gardenContainers {
		containersByGuid[gardenContainer.Guid] = gardenContainer
	}
	for _, allocatedContainer := range allocatedContainers {
		containersByGuid[allocatedContainer.Guid] = allocatedContainer
	}

	containers := make([]executor.Container, 0, len(containersByGuid))
	for _, container := range containersByGuid {
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

	c.multiStoreLock.Lock()
	defer c.multiStoreLock.Unlock()

	allocationStoreErr := c.allocationStore.Deallocate(logger, guid)
	if allocationStoreErr != nil {
		logger.Debug("did-not-delete-allocation-container", lager.Data{"message": allocationStoreErr.Error()})
	}

	if _, err := c.gardenStore.Lookup(guid); err != nil {
		logger.Debug("garden-container-not-found", lager.Data{"message": err.Error()})
		return allocationStoreErr
	} else if err := c.gardenStore.Destroy(logger, guid); err != nil {
		logger.Error("failed-to-delete-garden-container", err)
		return err
	}

	return nil
}

func (c *client) remainingResources() (executor.ExecutorResources, error) {
	remainingResources, err := c.TotalResources()
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	allocatedContainers := c.allocationStore.List()
	fmt.Printf("number of allocated containers:%d\n", len(allocatedContainers))
	for _, allocation := range allocatedContainers {
		fmt.Printf("allocated container guid:%s\n", allocation.Guid)
		fmt.Printf("allocated container disk MB:%d\n", allocation.DiskMB)
		fmt.Printf("allocated container memory MB:%d\n", allocation.MemoryMB)
		remainingResources.Containers--
		remainingResources.DiskMB -= allocation.DiskMB
		remainingResources.MemoryMB -= allocation.MemoryMB
		fmt.Printf("remaining disk MB:%d\n", remainingResources.DiskMB)
		fmt.Printf("remaining memory MB:%d\n", remainingResources.MemoryMB)
	}

	gardenContainers, err := c.gardenStore.List(nil)
	if err != nil {
		return executor.ExecutorResources{}, err
	}
	fmt.Printf("number of garden containers:%d\n", len(gardenContainers))

	for _, gardenContainer := range gardenContainers {
		fmt.Printf("garden container disk MB:%d\n", gardenContainer.DiskMB)
		fmt.Printf("garden container memory MB:%d\n", gardenContainer.MemoryMB)
		remainingResources.Containers--
		remainingResources.DiskMB -= gardenContainer.DiskMB
		remainingResources.MemoryMB -= gardenContainer.MemoryMB
		fmt.Printf("remaining disk MB:%d\n", remainingResources.DiskMB)
		fmt.Printf("remaining memory MB:%d\n", remainingResources.MemoryMB)
	}

	return remainingResources, nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	c.multiStoreLock.Lock()
	defer c.multiStoreLock.Unlock()

	return c.remainingResources()
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
	remainingResources, err := c.remainingResources()
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
