package depot

import (
	"os"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
	"github.com/tedsuo/ifrit"
)

type client struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	wardenClient          warden.Client
	registry              registry.Registry
	transformer           *transformer.Transformer
	logger                *gosteno.Logger
}

func NewClient(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	logger *gosteno.Logger,
) api.Client {
	return &client{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		wardenClient:          wardenClient,
		registry:              registry,
		transformer:           transformer,
		logger:                logger,
	}
}

func (c *client) InitializeContainer(guid string, request api.ContainerInitializationRequest) (api.Container, error) {
	if request.CpuPercent > 100 || request.CpuPercent < 0 {
		return api.Container{}, api.ErrLimitsInvalid
	}

	reg, err := c.registry.FindByGuid(guid)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.not-found")
		return api.Container{}, api.ErrContainerNotFound
	}

	containerClient, err := c.wardenClient.Create(warden.ContainerSpec{
		Properties: warden.Properties{
			"owner": c.containerOwnerName,
		},
	})
	if err != nil {
		c.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.create-failed")
		return api.Container{}, err
	}

	err = c.limitContainerDiskAndMemory(reg, containerClient)
	if err != nil {
		c.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.limit-disk-and-memory-failed")
		return api.Container{}, err
	}

	err = c.limitContainerCPU(request, containerClient)
	if err != nil {
		c.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.limit-cpu-failed")
		return api.Container{}, err
	}

	portMapping, err := c.mapPorts(request, containerClient)
	if err != nil {
		c.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.port-mapping-failed")
		return api.Container{}, err
	}

	request.Ports = portMapping

	reg, err = c.registry.Create(reg.Guid, containerClient.Handle(), request)
	if err != nil {
		c.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.registry-failed")
		return api.Container{}, err
	}

	return reg, nil
}

func (c *client) limitContainerDiskAndMemory(reg api.Container, containerClient warden.Container) error {
	if reg.MemoryMB != 0 {
		err := containerClient.LimitMemory(warden.MemoryLimits{
			LimitInBytes: uint64(reg.MemoryMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	if reg.DiskMB != 0 {
		err := containerClient.LimitDisk(warden.DiskLimits{
			ByteHard: uint64(reg.DiskMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *client) limitContainerCPU(request api.ContainerInitializationRequest, containerClient warden.Container) error {
	if request.CpuPercent != 0 {
		err := containerClient.LimitCPU(warden.CPULimits{
			LimitInShares: uint64(float64(c.containerMaxCPUShares) * float64(request.CpuPercent) / 100.0),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *client) mapPorts(request api.ContainerInitializationRequest, containerClient warden.Container) ([]api.PortMapping, error) {
	var result []api.PortMapping
	for _, mapping := range request.Ports {
		hostPort, containerPort, err := containerClient.NetIn(mapping.HostPort, mapping.ContainerPort)
		if err != nil {
			return nil, err
		}

		result = append(result, api.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
		})
	}

	return result, nil
}

func (c *client) AllocateContainer(guid string, request api.ContainerAllocationRequest) (api.Container, error) {
	container, err := c.registry.Reserve(guid, request)
	if err == registry.ErrContainerAlreadyExists {
		c.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
			"guid":  guid,
		}, "executor.allocate-container.container-already-exists")
		return api.Container{}, api.ErrContainerGuidNotAvailable
	}

	if err != nil {
		c.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.full")
		return api.Container{}, api.ErrInsufficientResourcesAvailable
	}

	return container, nil
}

func (c *client) GetContainer(guid string) (api.Container, error) {
	container, err := c.registry.FindByGuid(guid)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.get-container.not-found")
		return api.Container{}, api.ErrContainerNotFound
	}
	return container, nil
}

func (c *client) Run(guid string, request api.ContainerRunRequest) error {
	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.container-not-found")
		return api.ErrContainerNotFound
	}

	container, err := c.wardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.lookup-failed")
		return err
	}

	var result string
	steps, err := c.transformer.StepsFor(registration.Log, request.Actions, container, &result)
	if err != nil {
		c.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.steps-invalid")
		return api.ErrStepsInvalid
	}

	run := RunSequence{
		CompleteURL:  request.CompleteURL,
		Registration: registration,
		Sequence:     sequence.New(steps),
		Result:       &result,
		Registry:     c.registry,
		Logger:       c.logger,
	}
	process := ifrit.Envoke(run)
	c.registry.Start(run.Registration.Guid, process)

	c.logger.Infod(map[string]interface{}{
		"guid":   guid,
		"handle": registration.ContainerHandle,
	}, "executor.run-actions.started")
	return nil
}

func (c *client) ListContainers() ([]api.Container, error) {
	return c.registry.GetAllContainers(), nil
}

func (c *client) DeleteContainer(guid string) error {
	reg, err := c.registry.FindByGuid(guid)
	if err != nil {
		return handleDeleteError(err, c.logger)
	}

	if reg.Process != nil {
		reg.Process.Signal(os.Interrupt)
		<-reg.Process.Wait()
	}

	if reg.ContainerHandle != "" {
		err = c.wardenClient.Destroy(reg.ContainerHandle)
		if err != nil {
			return handleDeleteError(err, c.logger)
		}
	}

	err = c.registry.Delete(guid)
	if err != nil {
		return handleDeleteError(err, c.logger)
	}

	return nil
}

func (c *client) RemainingResources() (api.ExecutorResources, error) {
	cap := c.registry.CurrentCapacity()

	return api.ExecutorResources{
		MemoryMB:   cap.MemoryMB,
		DiskMB:     cap.DiskMB,
		Containers: cap.Containers,
	}, nil
}

func (c *client) Ping() error {
	return c.wardenClient.Ping()
}

func (c *client) TotalResources() (api.ExecutorResources, error) {
	totalCapacity := c.registry.TotalCapacity()

	resources := api.ExecutorResources{
		MemoryMB:   totalCapacity.MemoryMB,
		DiskMB:     totalCapacity.DiskMB,
		Containers: totalCapacity.Containers,
	}
	return resources, nil
}

func handleDeleteError(err error, logger *gosteno.Logger) error {
	if err == registry.ErrContainerNotFound {
		logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.delete-container.not-found")
		return api.ErrContainerNotFound
	}

	logger.Errord(map[string]interface{}{
		"error": err.Error(),
	}, "executor.delete-container.failed")
	return err
}
