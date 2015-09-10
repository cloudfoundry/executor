package depot

import (
	"io"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/keyed_lock"
	"github.com/cloudfoundry/gunk/workpool"
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
	creationWorkPool     *workpool.WorkPool
	deletionWorkPool     *workpool.WorkPool
	readWorkPool         *workpool.WorkPool
	metricsWorkPool      *workpool.WorkPool
}

type AllocationStore interface {
	List() []executor.Container
	Lookup(guid string) (executor.Container, error)
	Allocate(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error)
	Initialize(logger lager.Logger, req *executor.RunRequest) error
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
	workPoolSettings executor.WorkPoolSettings,
) (executor.ClientProvider, error) {
	creationWorkPool, err := workpool.NewWorkPool(workPoolSettings.CreateWorkPoolSize)
	if err != nil {
		return nil, err
	}
	deletionWorkPool, err := workpool.NewWorkPool(workPoolSettings.DeleteWorkPoolSize)
	if err != nil {
		return nil, err
	}
	readWorkPool, err := workpool.NewWorkPool(workPoolSettings.ReadWorkPoolSize)
	if err != nil {
		return nil, err
	}
	metricsWorkPool, err := workpool.NewWorkPool(workPoolSettings.MetricsWorkPoolSize)
	if err != nil {
		return nil, err
	}

	return &clientProvider{
		totalCapacity:        totalCapacity,
		allocationStore:      allocationStore,
		gardenStore:          gardenStore,
		eventHub:             eventHub,
		containerLockManager: lockManager,
		resourcesLock:        new(sync.Mutex),
		creationWorkPool:     creationWorkPool,
		deletionWorkPool:     deletionWorkPool,
		readWorkPool:         readWorkPool,
		metricsWorkPool:      metricsWorkPool,
	}, nil
}

func (provider *clientProvider) WithLogger(logger lager.Logger) executor.Client {
	return &client{
		provider,
		logger.Session("depot-client"),
	}
}

func (c *client) Cleanup() {
	c.creationWorkPool.Stop()
	c.deletionWorkPool.Stop()
	c.readWorkPool.Stop()
	c.metricsWorkPool.Stop()
}

func (c *client) AllocateContainers(requests []executor.AllocationRequest) (map[string]string, error) {
	c.resourcesLock.Lock()
	defer c.resourcesLock.Unlock()

	logger := c.logger.Session("allocate-containers")
	errMessageMap := map[string]string{}

	remainingResources, err := c.remainingResources(logger)
	if err != nil {
		return nil, executor.ErrFailureToCheckSpace
	}

	for i := range requests {
		// if executorContainer.CPUWeight > 100 || executorContainer.CPUWeight < 0 {
		// 	logger.Debug("invalid-cpu-weight", lager.Data{
		// 		"guid":      executorContainer.Guid,
		// 		"cpuweight": executorContainer.CPUWeight,
		// 	})
		// 	errMessageMap[executorContainer.Guid] = executor.ErrLimitsInvalid.Error()
		// 	continue
		// } else if executorContainer.CPUWeight == 0 {
		// 	executorContainer.CPUWeight = 100
		// }

		req := &requests[i]
		err := req.Validate()
		if err != nil {
			logger.Debug("empty-guid")
			// TODO: return a slice of errors instead of a map so we can handle this case
			errMessageMap[""] = err.Error()
			continue
		}

		ok := remainingResources.Subtract(&req.Resource)
		if !ok {
			logger.Debug("failed-to-allocate-container", lager.Data{"guid": req.Guid, "error": executor.ErrInsufficientResourcesAvailable.Error()})
			errMessageMap[req.Guid] = executor.ErrInsufficientResourcesAvailable.Error()
			continue
		}

		_, err = c.allocationStore.Allocate(logger, req)
		if err != nil {
			logger.Debug("failed-to-allocate-container", lager.Data{"guid": req.Guid, "error": err.Error()})
			errMessageMap[req.Guid] = err.Error()
			continue
		}
	}

	return errMessageMap, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get-container", lager.Data{
		"guid": guid,
	})

	container, err := c.allocationStore.Lookup(guid)
	if err != nil {
		errChannel := make(chan error, 1)
		containerChannel := make(chan executor.Container, 1)

		c.readWorkPool.Submit(func() {
			c.containerLockManager.Lock(guid)
			defer c.containerLockManager.Unlock(guid)

			container, err = c.gardenStore.Lookup(logger, guid)
			if err != nil {
				errChannel <- err
				return
			}
			containerChannel <- container
			return
		})

		select {
		case container = <-containerChannel:
			err = nil
		case err = <-errChannel:
			container = executor.Container{}
		}
		close(errChannel)
		close(containerChannel)
	}

	return container, err
}

func (c *client) RunContainer(request *executor.RunRequest) error {
	logger := c.logger.Session("run-container", lager.Data{
		"guid": request.Guid,
	})

	logger.Debug("initializing-container")
	err := c.allocationStore.Initialize(logger, request)
	if err != nil {
		logger.Error("failed-initializing-container", err)
		return err
	}
	logger.Debug("succeeded-initializing-container")

	c.creationWorkPool.Submit(c.newRunContainerWorker(request.Guid, logger))
	return nil
}

func (c *client) newRunContainerWorker(guid string, logger lager.Logger) func() {
	return func() {
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

		logger.Info("creating-container-in-garden")
		container, err = c.gardenStore.Create(logger, container)
		if err != nil {
			logger.Error("failed-creating-container-in-garden", err)
			c.allocationStore.Fail(logger, guid, ContainerInitializationFailedMessage)
			return
		}
		logger.Info("succeeded-creating-container-in-garden")

		if !c.allocationStore.Deallocate(logger, guid) {
			logger.Info("container-deallocated-during-initialization")

			err = c.gardenStore.Destroy(logger, guid)
			if err != nil {
				logger.Error("failed-to-destroy", err)
			}

			return
		}

		logger.Info("running-container-in-garden")
		err = c.gardenStore.Run(logger, container)
		if err != nil {
			logger.Error("failed-running-container-in-garden", err)
		}
		logger.Info("succeeded-running-container-in-garden")
	}
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
	errChannel := make(chan error, 1)
	containersChannel := make(chan []executor.Container, 1)
	c.readWorkPool.Submit(func() {
		gardenContainers, err := c.gardenStore.List(logger, tags)
		if err != nil {
			logger.Error("failed-listing-garden-store", err)
			errChannel <- err
		} else {
			logger.Debug("succeeded-listing-garden-store")
			containersChannel <- gardenContainers
		}
	})

	var err error
	var gardenContainers []executor.Container
	select {
	case gardenContainers = <-containersChannel:
		err = nil
	case err = <-errChannel:
	}
	close(errChannel)
	close(containersChannel)

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

func (c *client) GetMetrics(guid string) (executor.ContainerMetrics, error) {

	errChannel := make(chan error, 1)
	metricsChannel := make(chan executor.ContainerMetrics, 1)

	logger := c.logger.Session("get-metrics")

	c.metricsWorkPool.Submit(func() {
		metrics, err := c.gardenStore.Metrics(logger, []string{guid})
		if err != nil {
			logger.Error("failed-get-container-metrics", err)
			errChannel <- err
		} else {
			if m, found := metrics[guid]; found {
				metricsChannel <- m
			} else {
				errChannel <- executor.ErrContainerNotFound
			}
		}
	})

	var metrics executor.ContainerMetrics
	var err error
	select {
	case metrics = <-metricsChannel:
		err = nil
	case err = <-errChannel:
		metrics = executor.ContainerMetrics{}
	}

	close(metricsChannel)
	close(errChannel)
	return metrics, err
}

func (c *client) GetAllMetrics(tags executor.Tags) (map[string]executor.Metrics, error) {

	errChannel := make(chan error, 1)
	metricsChannel := make(chan map[string]executor.Metrics, 1)

	logger := c.logger.Session("get-all-metrics")

	c.metricsWorkPool.Submit(func() {
		containers, err := c.gardenStore.List(logger, tags)
		if err != nil {
			logger.Error("failed-to-list-containers", err)
			errChannel <- err
			return
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
			errChannel <- err
			return
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
		metricsChannel <- metrics
	})

	var metrics map[string]executor.Metrics
	var err error
	select {
	case metrics = <-metricsChannel:
		err = nil
	case err = <-errChannel:
		metrics = make(map[string]executor.Metrics)
	}

	close(metricsChannel)
	close(errChannel)
	return metrics, err
}

func (c *client) StopContainer(guid string) error {
	errChannel := make(chan error, 1)

	logger := c.logger.Session("stop-container", lager.Data{
		"guid": guid,
	})

	container, err := c.allocationStore.Lookup(guid)
	if err == executor.ErrContainerNotFound {
		c.deletionWorkPool.Submit(func() {
			c.containerLockManager.Lock(guid)
			defer c.containerLockManager.Unlock(guid)

			err = c.gardenStore.Stop(logger, guid)
			if err != nil {
				logger.Error("failed-to-find-container", err)
				errChannel <- err
			} else {
				errChannel <- nil
			}
		})
		err = <-errChannel
		close(errChannel)
	}
	if container.State != executor.StateCompleted {
		c.allocationStore.Fail(logger, guid, ContainerStoppedBeforeRunMessage)
	}

	return err
}

func (c *client) DeleteContainer(guid string) error {
	errChannel := make(chan error, 1)
	c.deletionWorkPool.Submit(func() {
		logger := c.logger.Session("delete-container", lager.Data{
			"guid": guid,
		})

		c.containerLockManager.Lock(guid)
		defer c.containerLockManager.Unlock(guid)

		// don't care if we actually deallocated
		c.allocationStore.Deallocate(logger, guid)

		if err := c.gardenStore.Destroy(logger, guid); err != nil {
			logger.Error("failed-to-delete-garden-container", err)
			errChannel <- err
		} else {
			errChannel <- nil
		}
	})

	err := <-errChannel
	close(errChannel)

	return err
}

func (c *client) remainingResources(logger lager.Logger) (executor.ExecutorResources, error) {
	remainingResources, err := c.TotalResources()
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	processedGuids := map[string]struct{}{}

	allocatedContainers := c.allocationStore.List()
	for i := range allocatedContainers {
		c := &allocatedContainers[i]
		processedGuids[c.Guid] = struct{}{}
		remainingResources.Subtract(&c.Resource)
	}

	gardenContainers, err := c.gardenStore.List(logger, nil)
	if err != nil {
		return executor.ExecutorResources{}, err
	}

	for i := range gardenContainers {
		c := &gardenContainers[i]
		if _, seen := processedGuids[c.Guid]; seen {
			continue
		}
		processedGuids[c.Guid] = struct{}{}
		remainingResources.Subtract(&c.Resource)
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

	errChannel := make(chan error, 1)
	readChannel := make(chan io.ReadCloser, 1)
	c.readWorkPool.Submit(func() {
		readCloser, err := c.gardenStore.GetFiles(logger, guid, sourcePath)
		if err != nil {
			errChannel <- err
		} else {
			readChannel <- readCloser
		}
	})

	var readCloser io.ReadCloser
	var err error
	select {
	case readCloser = <-readChannel:
		err = nil
	case err = <-errChannel:
	}
	return readCloser, err
}

func (c *client) SubscribeToEvents() (executor.EventSource, error) {
	return c.eventHub.Subscribe()
}
