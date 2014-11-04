package depot

import (
	"fmt"
	"io"
	"sync"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/store"
	"github.com/pivotal-golang/lager"
)

const ContainerInitializationFailedMessage = "failed to initialize container: %s"

type AllocationsTracker interface {
	Allocations() []executor.Container
}

type client struct {
	logger lager.Logger

	totalCapacity   executor.ExecutorResources
	gardenStore     GardenStore
	allocationStore AllocationStore
	tracker         AllocationsTracker

	resourcesL sync.Mutex
}

type Store interface {
	Create(executor.Container) (executor.Container, error)
	Lookup(guid string) (executor.Container, error)
	List(executor.Tags) ([]executor.Container, error)
	Destroy(guid string) error
	Complete(guid string, result executor.ContainerRunResult) error
}

type AllocationStore interface {
	Store

	StartInitializing(guid string) error
}

type GardenStore interface {
	Ping() error

	Store

	Run(executor.Container, func(executor.ContainerRunResult)) error
	GetFiles(guid, sourcePath string) (io.ReadCloser, error)
}

func NewClient(
	totalCapacity executor.ExecutorResources,
	gardenStore GardenStore,
	allocationStore AllocationStore,
	tracker AllocationsTracker,
	logger lager.Logger,
) executor.Client {
	return &client{
		totalCapacity:   totalCapacity,
		gardenStore:     gardenStore,
		allocationStore: allocationStore,
		tracker:         tracker,
		logger:          logger.Session("depot-client"),
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

	createdContainer, err := c.allocationStore.Create(executorContainer)
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

	container, err := c.allocationStore.Lookup(guid)
	if err != nil {
		logger.Error("failed-to-find-container", err)
		return executor.ErrContainerNotFound
	}

	err = c.allocationStore.StartInitializing(container.Guid)
	if err != nil {
		logger.Error("failed-to-initialize-container", err)
		return executor.ErrContainerNotFound
	}

	go func() {
		container, err := c.gardenStore.Create(container)
		if err != nil {
			logger.Error("failed-to-create-container", err)

			c.allocationStore.Complete(guid, executor.ContainerRunResult{
				Failed:        true,
				FailureReason: fmt.Sprintf(ContainerInitializationFailedMessage, err.Error()),
			})

			return
		}

		err = c.allocationStore.Destroy(guid)
		if err == store.ErrContainerNotFound {
			logger.Debug("container-deleted-while-initilizing")

			err := c.gardenStore.Destroy(container.Guid)
			if err != nil {
				logger.Error("failed-to-destroy-newly-initialized-container", err)
			}

			logger.Info("destroyed-newly-initialized-container")

			return
		} else if err != nil {
			logger.Error("failed-to-remove-allocated-container-for-some-reason", err)
			return
		}

		err = c.gardenStore.Run(container, func(result executor.ContainerRunResult) {
			err := c.gardenStore.Complete(container.Guid, result)
			if err != nil {
				// not a lot we can do here
				logger.Error("failed-to-save-result", err)
			}
		})

		if err != nil {
			logger.Error("failed-to-run-container", err)
			return
		}
	}()

	return nil
}

func (c *client) ListContainers(tags executor.Tags) ([]executor.Container, error) {
	containersByHandle := make(map[string]executor.Container)

	gardenContainers, err := c.gardenStore.List(tags)
	if err != nil {
		return nil, err
	}

	allocatedContainers, err := c.allocationStore.List(tags)
	if err != nil {
		return nil, err
	}

	for _, gardenContainer := range append(gardenContainers, allocatedContainers...) {
		containersByHandle[gardenContainer.Guid] = gardenContainer
	}

	var containers []executor.Container
	for _, container := range containersByHandle {
		containers = append(containers, container)
	}

	return containers, nil
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete", lager.Data{
		"guid": guid,
	})

	if err := c.allocationStore.Destroy(guid); err == nil {
		logger.Info("deleted-allocation")
		return nil
	}

	err := c.gardenStore.Destroy(guid)
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

	for _, banana := range c.tracker.Allocations() {
		remainingResources.Containers--
		remainingResources.DiskMB -= banana.DiskMB
		remainingResources.MemoryMB -= banana.MemoryMB
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
