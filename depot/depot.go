package depot

import (
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
	lockMap         map[string]*sync.Mutex
	lockForLockMap  *sync.Mutex
	resourcesLock   *sync.Mutex
}

type AllocationStore interface {
	List() []executor.Container
	Lookup(guid string) (executor.Container, error)
	Allocate(logger lager.Logger, container executor.Container) (executor.Container, error)
	Initialize(logger lager.Logger, guid string) error
	Fail(logger lager.Logger, guid string, reason string) (executor.Container, error)
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
		lockMap:         map[string]*sync.Mutex{},
		lockForLockMap:  new(sync.Mutex),
		resourcesLock:   new(sync.Mutex),
	}
}

func (provider *clientProvider) WithLogger(logger lager.Logger) executor.Client {
	return &client{
		provider,
		logger.Session("depot-client"),
	}
}

func (c *client) getLock(guid string) *sync.Mutex {
	var lock *sync.Mutex
	var found bool

	c.lockForLockMap.Lock()
	defer c.lockForLockMap.Unlock()

	if lock, found = c.lockMap[guid]; !found {
		lock = new(sync.Mutex)
		c.lockMap[guid] = lock
	}

	return lock
}

func (c *client) removeLockFromMap(guid string) {
	c.lockForLockMap.Lock()
	defer c.lockForLockMap.Unlock()
	delete(c.lockMap, guid)
}

func (c *client) AllocateContainers(executorContainers []executor.Container) (map[string]string, error) {
	logger := c.logger.Session("allocate")

	errMessageMap := map[string]string{}
	eligibleContainers := make([]executor.Container, 0, len(executorContainers))

	for _, executorContainer := range executorContainers {
		if executorContainer.CPUWeight > 100 || executorContainer.CPUWeight < 0 {
			logger.Debug("invalid-cpu-weight", lager.Data{
				"guid":      executorContainer.Guid,
				"cpuweight": executorContainer.CPUWeight,
			})
			errMessageMap[executorContainer.Guid] = executor.ErrLimitsInvalid.Error()
			continue
		} else if executorContainer.CPUWeight == 0 {
			executorContainer.CPUWeight = 100
		}

		if executorContainer.Guid == "" {
			logger.Debug("empty-guid")
			errMessageMap[executorContainer.Guid] = executor.ErrGuidNotSpecified.Error()
			continue
		}

		eligibleContainers = append(eligibleContainers, executorContainer)
	}

	c.resourcesLock.Lock()
	defer c.resourcesLock.Unlock()

	allocatableContainers, unallocatableContainers, err := c.checkSpace(eligibleContainers)
	if err != nil {
		logger.Error("failed-to-allocate-containers", err)
		return nil, executor.ErrFailureToCheckSpace
	}

	for _, allocatableContainer := range allocatableContainers {
		if allocatedContainer, err := c.allocationStore.Allocate(logger, allocatableContainer); err != nil {
			logger.Debug(
				"failed-to-allocate-container",
				lager.Data{
					"guid":         allocatableContainer.Guid,
					"errormessage": err.Error(),
				},
			)
			errMessageMap[allocatableContainer.Guid] = err.Error()
		} else {
			c.eventHub.EmitEvent(executor.NewContainerReservedEvent(allocatedContainer))
		}
	}

	for _, unallocatableContainer := range unallocatableContainers {
		logger.Debug(
			"failed-to-allocate-container",
			lager.Data{
				"guid":         unallocatableContainer.Guid,
				"errormessage": executor.ErrInsufficientResourcesAvailable.Error(),
			},
		)
		errMessageMap[unallocatableContainer.Guid] = executor.ErrInsufficientResourcesAvailable.Error()
	}

	return errMessageMap, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	lock := c.getLock(guid)
	lock.Lock()
	defer lock.Unlock()

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
		lock := c.getLock(guid)
		lock.Lock()
		defer lock.Unlock()

		container, err := c.allocationStore.Lookup(guid)
		if err != nil {
			logger.Error("failed-to-find-container", err)
			return
		}

		container, err = c.gardenStore.Create(logger, container)
		if err != nil {
			logger.Error("failed-to-create-container", err)

			failedContainer, _ := c.allocationStore.Fail(logger, guid, ContainerInitializationFailedMessage)
			c.eventHub.EmitEvent(executor.NewContainerCompleteEvent(failedContainer))

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

	lock := c.getLock(guid)
	lock.Lock()
	defer lock.Unlock()

	allocationStoreErr := c.allocationStore.Deallocate(logger, guid)
	if allocationStoreErr != nil {
		logger.Debug("did-not-delete-allocation-container", lager.Data{"message": allocationStoreErr.Error()})
	}

	if _, err := c.gardenStore.Lookup(guid); err != nil {
		logger.Debug("garden-container-not-found", lager.Data{"message": err.Error()})
		c.removeLockFromMap(guid)
		return allocationStoreErr
	} else if err := c.gardenStore.Destroy(logger, guid); err != nil {
		logger.Error("failed-to-delete-garden-container", err)
		return err
	}

	c.removeLockFromMap(guid)
	return nil
}

func (c *client) remainingResources() (executor.ExecutorResources, error) {
	remainingResources, err := c.TotalResources()
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	allocatedContainers := c.allocationStore.List()
	for _, allocation := range allocatedContainers {
		remainingResources.Containers--
		remainingResources.DiskMB -= allocation.DiskMB
		remainingResources.MemoryMB -= allocation.MemoryMB
	}

	gardenContainers, err := c.gardenStore.List(nil)
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	for _, gardenContainer := range gardenContainers {
		remainingResources.Containers--
		remainingResources.DiskMB -= gardenContainer.DiskMB
		remainingResources.MemoryMB -= gardenContainer.MemoryMB
	}

	return remainingResources, nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	c.resourcesLock.Lock()
	defer c.resourcesLock.Unlock()

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

func (c *client) checkSpace(containers []executor.Container) ([]executor.Container, []executor.Container, error) {
	remainingResources, err := c.remainingResources()
	if err != nil {
		return nil, nil, err
	}

	allocatableContainers := make([]executor.Container, 0, len(containers))
	unallocatableContainers := make([]executor.Container, 0, len(containers))

	// Can be optimized to select containers such that maximum number of containers can be allocated
	for _, container := range containers {
		if remainingResources.MemoryMB < container.MemoryMB {
			unallocatableContainers = append(unallocatableContainers, container)
			continue
		}

		if remainingResources.DiskMB < container.DiskMB {
			unallocatableContainers = append(unallocatableContainers, container)
			continue
		}

		if remainingResources.Containers < 1 {
			unallocatableContainers = append(unallocatableContainers, container)
			continue
		}

		remainingResources.MemoryMB -= container.MemoryMB
		remainingResources.DiskMB -= container.DiskMB
		remainingResources.Containers--
		allocatableContainers = append(allocatableContainers, container)
	}

	return allocatableContainers, unallocatableContainers, nil
}
