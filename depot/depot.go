package depot

import (
	"io"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/containerstore"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
	"github.com/cloudfoundry/gunk/workpool"
	"github.com/pivotal-golang/lager"
)

const ContainerStoppedBeforeRunMessage = "Container stopped by user"
const GardenContainerCreationDuration = metric.Duration("GardenContainerCreationDuration")

type client struct {
	*clientProvider
	logger lager.Logger
}

type clientProvider struct {
	totalCapacity    executor.ExecutorResources
	containerStore   containerstore.ContainerStore
	gardenStore      GardenStore
	eventHub         event.Hub
	creationWorkPool *workpool.WorkPool
	deletionWorkPool *workpool.WorkPool
	readWorkPool     *workpool.WorkPool
	metricsWorkPool  *workpool.WorkPool

	healthyLock sync.RWMutex
	healthy     bool
}

//go:generate counterfeiter -o fakes/fake_garden_store.go . GardenStore
type GardenStore interface {
	// This should probably live somewhere else.
	Ping() error
}

func NewClientProvider(
	totalCapacity executor.ExecutorResources,
	containerStore containerstore.ContainerStore,
	gardenStore GardenStore,
	eventHub event.Hub,
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
		totalCapacity:    totalCapacity,
		containerStore:   containerStore,
		gardenStore:      gardenStore,
		eventHub:         eventHub,
		creationWorkPool: creationWorkPool,
		deletionWorkPool: deletionWorkPool,
		readWorkPool:     readWorkPool,
		metricsWorkPool:  metricsWorkPool,
		healthy:          true,
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

func (c *client) AllocateContainers(requests []executor.AllocationRequest) ([]executor.AllocationFailure, error) {
	logger := c.logger.Session("allocate-containers")
	failures := make([]executor.AllocationFailure, 0)

	for i := range requests {
		req := &requests[i]
		err := req.Validate()
		if err != nil {
			logger.Error("invalid-request", err)
			failures = append(failures, executor.NewAllocationFailure(req, err.Error()))
			continue
		}

		_, err = c.containerStore.Reserve(logger, req)
		if err != nil {
			logger.Error("failed-to-allocate-container", err, lager.Data{"guid": req.Guid})
			failures = append(failures, executor.NewAllocationFailure(req, err.Error()))
			continue
		}
	}

	return failures, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get-container", lager.Data{
		"guid": guid,
	})

	container, err := c.containerStore.Get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
	}

	return container, err
}

func (c *client) RunContainer(request *executor.RunRequest) error {
	logger := c.logger.Session("run-container", lager.Data{
		"guid": request.Guid,
	})

	logger.Debug("initializing-container")
	err := c.containerStore.Initialize(logger, request)
	if err != nil {
		logger.Error("failed-initializing-container", err)
		return err
	}
	logger.Debug("succeeded-initializing-container")

	c.creationWorkPool.Submit(c.newRunContainerWorker(logger, request.Guid))
	return nil
}

func (c *client) newRunContainerWorker(logger lager.Logger, guid string) func() {
	return func() {
		logger.Info("creating-container")
		startTime := time.Now()
		_, err := c.containerStore.Create(logger, guid)
		if err != nil {
			logger.Error("failed-creating-container", err)
			return
		}
		GardenContainerCreationDuration.Send(time.Now().Sub(startTime))
		logger.Info("succeeded-creating-container-in-garden")

		logger.Info("running-container-in-garden")
		err = c.containerStore.Run(logger, guid)
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

func (c *client) ListContainers() ([]executor.Container, error) {
	return c.containerStore.List(c.logger), nil
}

func (c *client) GetBulkMetrics() (map[string]executor.Metrics, error) {
	errChannel := make(chan error, 1)
	metricsChannel := make(chan map[string]executor.Metrics, 1)

	logger := c.logger.Session("get-all-metrics")

	c.metricsWorkPool.Submit(func() {
		containers := c.containerStore.List(logger)
		containerGuids := make([]string, 0, len(containers))
		for _, container := range containers {
			if container.MetricsConfig.Guid != "" {
				containerGuids = append(containerGuids, container.Guid)
			}
		}

		cmetrics, err := c.containerStore.Metrics(logger)
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
	logger := c.logger.Session("stop-container")
	logger.Info("starting")
	defer logger.Info("complete")

	return c.containerStore.Stop(c.logger, guid)
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete-container", lager.Data{"guid": guid})

	logger.Info("starting")
	defer logger.Info("complete")

	errChannel := make(chan error, 1)
	c.deletionWorkPool.Submit(func() {
		errChannel <- c.containerStore.Destroy(logger, guid)
	})

	err := <-errChannel

	if err != nil {
		logger.Error("failed-to-delete-garden-container", err)
	}

	return err
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	logger := c.logger.Session("remaining-resources")
	return c.containerStore.RemainingResources(logger), nil
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
		readCloser, err := c.containerStore.GetFiles(logger, guid, sourcePath)
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

func (c *client) Healthy() bool {
	c.healthyLock.RLock()
	defer c.healthyLock.RUnlock()
	return c.healthy
}

func (c *client) SetHealthy(healthy bool) {
	c.healthyLock.Lock()
	defer c.healthyLock.Unlock()
	c.healthy = healthy
}
