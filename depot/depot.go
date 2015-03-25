package depot

import (
	"io"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/keyed_lock"
	"github.com/pivotal-golang/lager"
)

const ContainerInitializationFailedMessage = "failed to initialize container"
const ContainerStoppedBeforeRunMessage = "Container stopped by user"

type client struct {
	*clientProvider
	logger lager.Logger
}

type clientProvider struct {
	totalCapacity        executor.ExecutorResources
	gardenStore          GardenStore
	allocationStore      AllocationStore
	eventHub             event.Hub
	containerLockManager keyed_lock.LockManager
	resourcesLock        *sync.Mutex
}

type AllocationStore interface {
	List() []executor.Container
	Lookup(guid string) (executor.Container, error)
	Allocate(logger lager.Logger, container executor.Container) (executor.Container, error)
	Initialize(logger lager.Logger, guid string) error
	Fail(logger lager.Logger, guid string, reason string) (executor.Container, error)
	Deallocate(logger lager.Logger, guid string) bool
}

//go:generate counterfeiter -o fakes/fake_garden_store.go . GardenStore
type GardenStore interface {
	Create(logger lager.Logger, container executor.Container) (executor.Container, error)
	Lookup(logger lager.Logger, guid string) (executor.Container, error)
	List(logger lager.Logger, tags executor.Tags) ([]executor.Container, error)
	Metrics(logger lager.Logger, guid []string) (map[string]executor.ContainerMetrics, error)
	Destroy(logger lager.Logger, guid string) error
	Ping() error
	Run(logger lager.Logger, container executor.Container) error
	Stop(logger lager.Logger, guid string) error
	GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error)
}

func NewClientProvider(
	totalCapacity executor.ExecutorResources,
	allocationStore AllocationStore,
	gardenStore GardenStore,
	eventHub event.Hub,
	lockManager keyed_lock.LockManager,
) executor.ClientProvider {
	return &clientProvider{
		totalCapacity:        totalCapacity,
		allocationStore:      allocationStore,
		gardenStore:          gardenStore,
		eventHub:             eventHub,
		containerLockManager: lockManager,
		resourcesLock:        new(sync.Mutex),
	}
}

func (provider *clientProvider) WithLogger(logger lager.Logger) executor.Client {
	return &client{
		provider,
		logger.Session("depot-client"),
	}
}

func (c *client) AllocateContainers(executorContainers []executor.Container) (map[string]string, error) {
	logger := c.logger.Session("allocate-containers")

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

	allocatableContainers, unallocatableContainers, err := c.checkSpace(logger, eligibleContainers)
	if err != nil {
		logger.Error("failed-to-allocate-containers", err)
		return nil, executor.ErrFailureToCheckSpace
	}

	for _, allocatableContainer := range allocatableContainers {
		if _, err := c.allocationStore.Allocate(logger, allocatableContainer); err != nil {
			logger.Debug(
				"failed-to-allocate-container",
				lager.Data{
					"guid":  allocatableContainer.Guid,
					"error": err.Error(),
				},
			)
			errMessageMap[allocatableContainer.Guid] = err.Error()
		}
	}

	for _, unallocatableContainer := range unallocatableContainers {
		logger.Debug(
			"failed-to-allocate-container",
			lager.Data{
				"guid":  unallocatableContainer.Guid,
				"error": executor.ErrInsufficientResourcesAvailable.Error(),
			},
		)
		errMessageMap[unallocatableContainer.Guid] = executor.ErrInsufficientResourcesAvailable.Error()
	}

	return errMessageMap, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get-container", lager.Data{
		"guid": guid,
	})

	c.containerLockManager.Lock(guid)
	defer c.containerLockManager.Unlock(guid)

	container, err := c.allocationStore.Lookup(guid)
	if err != nil {
		container, err = c.gardenStore.Lookup(logger, guid)
		if err != nil {
			return executor.Container{}, err
		}
	}

	return container, nil
}

func (c *client) RunContainer(guid string) error {
	logger := c.logger.Session("run-container", lager.Data{
		"guid": guid,
	})

	logger.Debug("initializing-container")
	err := c.allocationStore.Initialize(logger, guid)
	if err != nil {
		logger.Error("failed-initializing-container", err)
		return err
	}
	logger.Debug("succeeded-initializing-container")

	go func() {
		c.containerLockManager.Lock(guid)
		defer c.containerLockManager.Unlock(guid)

		logger.Debug("looking-up-in-allocation-store")
		container, err := c.allocationStore.Lookup(guid)
		if err != nil {
			logger.Error("failed-looking-up-in-allocation-store", err)
			return
		}
		logger.Debug("succeeded-looking-up-in-allocation-store")

		if container.State != executor.StateInitializing {
			logger.Error("container-state-invalid", err, lager.Data{"state": container.State})
			return
		}

		logger.Debug("creating-container-in-garden")
		container, err = c.gardenStore.Create(logger, container)
		if err != nil {
			logger.Error("failed-creating-container-in-garden", err)
			c.allocationStore.Fail(logger, guid, ContainerInitializationFailedMessage)
			return
		}
		logger.Debug("succeeded-creating-container-in-garden")

		if !c.allocationStore.Deallocate(logger, guid) {
			logger.Info("container-deallocated-during-initialization")

			err = c.gardenStore.Destroy(logger, guid)
			if err != nil {
				logger.Error("failed-to-destroy", err)
			}

			return
		}

		logger.Debug("running-container")
		err = c.gardenStore.Run(logger, container)
		if err != nil {
			logger.Error("failed-running-container", err)
		}
		logger.Debug("succeeded-running-container")
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
	logger := c.logger.Session("list-containers", lager.Data{
		"tags": tags,
	})

	// Order here is important; listing containers from garden takes time, and in
	// that time a container may transition from allocation store to garden
	// store.
	//
	// In this case, if the garden store were fetched first, the container would
	// not be listed anywhere. This ordering guarantees that it will at least
	// show up allocated.
	logger.Debug("listing-allocation-store")
	allAllocatedContainers := c.allocationStore.List()
	logger.Debug("succeeded-listing-allocation-store")
	allocatedContainers := make([]executor.Container, 0, len(allAllocatedContainers))
	for _, container := range allAllocatedContainers {
		if tagsMatch(tags, container.Tags) {
			allocatedContainers = append(allocatedContainers, container)
		}
	}

	logger.Debug("listing-garden-store")
	gardenContainers, err := c.gardenStore.List(logger, tags)
	if err != nil {
		logger.Error("failed-listing-garden-store", err)
		return nil, err
	}
	logger.Debug("succeeded-listing-garden-store")

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

func (c *client) GetMetrics(guid string) (executor.ContainerMetrics, error) {
	logger := c.logger.Session("get-metrics")

	metrics, err := c.gardenStore.Metrics(logger, []string{guid})
	if err != nil {
		logger.Error("failed-get-container-metrics", err)
		return executor.ContainerMetrics{}, err
	}

	if m, found := metrics[guid]; found {
		return m, nil
	}

	return executor.ContainerMetrics{}, executor.ErrContainerNotFound
}

func (c *client) GetAllMetrics(tags executor.Tags) (map[string]executor.Metrics, error) {
	logger := c.logger.Session("get-all-metrics")

	containers, err := c.gardenStore.List(logger, tags)
	if err != nil {
		logger.Error("failed-to-list-containers", err)
		return nil, err
	}

	containerGuids := make([]string, 0, len(containers))
	for _, container := range containers {
		if container.MetricsConfig.Guid != "" {
			containerGuids = append(containerGuids, container.Guid)
		}
	}

	cmetrics, err := c.gardenStore.Metrics(logger, containerGuids)
	if err != nil {
		logger.Error("failed-to-get-metrics", err)
		return nil, err
	}

	metrics := make(map[string]executor.Metrics)
	for _, container := range containers {
		if container.MetricsConfig.Guid != "" {
			if cmetric, found := cmetrics[container.Guid]; found {
				metrics[container.Guid] = executor.Metrics{
					MetricsConfig:    container.MetricsConfig,
					ContainerMetrics: cmetric,
				}
			}
		}
	}

	return metrics, nil
}

func (c *client) StopContainer(guid string) error {
	logger := c.logger.Session("stop-container", lager.Data{
		"guid": guid,
	})

	c.containerLockManager.Lock(guid)
	defer c.containerLockManager.Unlock(guid)

	container, err := c.allocationStore.Lookup(guid)
	if err == executor.ErrContainerNotFound {
		return c.gardenStore.Stop(logger, guid)
	}
	if err != nil {
		logger.Error("failed-to-find-container", err)
		return err
	}

	if container.State != executor.StateCompleted {
		c.allocationStore.Fail(logger, guid, ContainerStoppedBeforeRunMessage)
	}

	return nil
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete-container", lager.Data{
		"guid": guid,
	})

	c.containerLockManager.Lock(guid)
	defer c.containerLockManager.Unlock(guid)

	// don't care if we actually deallocated
	c.allocationStore.Deallocate(logger, guid)

	if err := c.gardenStore.Destroy(logger, guid); err != nil {
		logger.Error("failed-to-delete-garden-container", err)
		return err
	}

	return nil
}

func (c *client) remainingResources(logger lager.Logger) (executor.ExecutorResources, error) {
	remainingResources, err := c.TotalResources()
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	processedGuids := map[string]struct{}{}

	allocatedContainers := c.allocationStore.List()
	for _, allocation := range allocatedContainers {
		processedGuids[allocation.Guid] = struct{}{}
		remainingResources.Containers--
		remainingResources.DiskMB -= allocation.DiskMB
		remainingResources.MemoryMB -= allocation.MemoryMB
	}

	gardenContainers, err := c.gardenStore.List(logger, nil)
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	for _, gardenContainer := range gardenContainers {
		if _, seen := processedGuids[gardenContainer.Guid]; seen {
			continue
		}
		remainingResources.Containers--
		remainingResources.DiskMB -= gardenContainer.DiskMB
		remainingResources.MemoryMB -= gardenContainer.MemoryMB
	}

	return remainingResources, nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	c.resourcesLock.Lock()
	defer c.resourcesLock.Unlock()

	return c.remainingResources(c.logger)
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
	logger := c.logger.Session("get-files", lager.Data{
		"guid": guid,
	})

	return c.gardenStore.GetFiles(logger, guid, sourcePath)
}

func (c *client) SubscribeToEvents() (executor.EventSource, error) {
	return c.eventHub.Subscribe()
}

func (c *client) checkSpace(logger lager.Logger, containers []executor.Container) ([]executor.Container, []executor.Container, error) {
	remainingResources, err := c.remainingResources(logger)
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
