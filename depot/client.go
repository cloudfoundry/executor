package depot

import (
	"os"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type client struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	containerInodeLimit   uint64
	wardenClient          warden.Client
	registry              registry.Registry
	transformer           *transformer.Transformer
	logger                lager.Logger
}

func NewClient(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	logger lager.Logger,
) api.Client {
	return &client{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		containerInodeLimit:   containerInodeLimit,
		wardenClient:          wardenClient,
		registry:              registry,
		transformer:           transformer,
		logger:                logger.Session("depot-client"),
	}
}

func (c *client) InitializeContainer(guid string, request api.ContainerInitializationRequest) (api.Container, error) {
	if request.CpuPercent > 100 || request.CpuPercent < 0 {
		return api.Container{}, api.ErrLimitsInvalid
	}

	initLog := c.logger.Session("initialize", lager.Data{
		"guid": guid,
	})

	container, err := c.registry.FindByGuid(guid)
	if err != nil {
		initLog.Error("failed-to-find-container", err)
		return api.Container{}, api.ErrContainerNotFound
	}

	container, err = c.registry.Initialize(guid)
	if err != nil {
		initLog.Error("failed-to-initialize-registry-container", err)
		return api.Container{}, err
	}

	containerClient, err := c.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: request.RootFSPath,
		Properties: warden.Properties{
			"owner": c.containerOwnerName,
		},
	})
	if err != nil {
		initLog.Error("failed-to-create-container", err)
		return api.Container{}, err
	}

	defer func() {
		if err != nil {
			initLog.Error("destroying-container-after-failed-init", err)
			destroyErr := c.wardenClient.Destroy(containerClient.Handle())
			if destroyErr != nil {
				initLog.Error("destroying-container-after-failed-init-also-failed", destroyErr)
			}
		}
	}()

	err = c.limitContainerDiskAndMemory(container, containerClient)
	if err != nil {
		initLog.Error("failed-to-limit-memory-and-disk", err)
		return api.Container{}, err
	}

	err = c.limitContainerCPU(request, containerClient)
	if err != nil {
		initLog.Error("failed-to-limit-cpu", err)
		return api.Container{}, err
	}

	portMapping, err := c.mapPorts(request, containerClient)
	if err != nil {
		initLog.Error("failed-to-map-ports", err)
		return api.Container{}, err
	}

	request.Ports = portMapping

	container, err = c.registry.Create(guid, containerClient.Handle(), request)
	if err != nil {
		initLog.Error("failed-to-register-container", err)
		return api.Container{}, err
	}

	return container, nil
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

	err := containerClient.LimitDisk(warden.DiskLimits{
		ByteHard:  uint64(reg.DiskMB * 1024 * 1024),
		InodeHard: c.containerInodeLimit,
	})
	if err != nil {
		return err
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
	allocLog := c.logger.Session("allocate", lager.Data{
		"guid": guid,
	})

	container, err := c.registry.Reserve(guid, request)
	if err == registry.ErrContainerAlreadyExists {
		allocLog.Error("container-already-allocated", err)
		return api.Container{}, api.ErrContainerGuidNotAvailable
	}

	if err != nil {
		allocLog.Error("full", err)
		return api.Container{}, api.ErrInsufficientResourcesAvailable
	}

	return container, nil
}

func (c *client) GetContainer(guid string) (api.Container, error) {
	getLog := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	container, err := c.registry.FindByGuid(guid)
	if err != nil {
		getLog.Error("container-not-found", err)
		return api.Container{}, api.ErrContainerNotFound
	}
	return container, nil
}

func (c *client) Run(guid string, request api.ContainerRunRequest) error {
	runLog := c.logger.Session("run", lager.Data{
		"guid": guid,
	})

	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		runLog.Error("container-not-found", err)
		return api.ErrContainerNotFound
	}

	container, err := c.wardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		runLog.Error("lookup-failed", err)
		return err
	}

	var result string
	steps, err := c.transformer.StepsFor(registration.Log, request.Actions, request.Env, container, &result)
	if err != nil {
		runLog.Error("steps-invalid", err)
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

	runLog.Info("started", lager.Data{
		"handle": registration.ContainerHandle,
	})

	return nil
}

func (c *client) ListContainers() ([]api.Container, error) {
	return c.registry.GetAllContainers(), nil
}

func (c *client) DeleteContainer(guid string) error {
	logData := lager.Data{
		"guid": guid,
	}

	deleteLog := c.logger.Session("delete", logData)

	reg, err := c.registry.MarkForDelete(guid)
	if err != nil {
		return handleDeleteError(err, deleteLog)
	}

	logData["handle"] = reg.ContainerHandle

	deleteLog.Debug("deleting")

	if reg.Process != nil {
		deleteLog.Debug("interrupting")

		reg.Process.Signal(os.Interrupt)
		<-reg.Process.Wait()

		deleteLog.Info("interrupted")
	}

	if reg.ContainerHandle != "" {
		deleteLog.Debug("destroying")

		err = c.wardenClient.Destroy(reg.ContainerHandle)
		if err != nil {
			return handleDeleteError(err, deleteLog)
		}

		deleteLog.Info("destroyed")
	}

	deleteLog.Debug("unregistering")

	err = c.registry.Delete(guid)
	if err != nil {
		return handleDeleteError(err, deleteLog)
	}

	deleteLog.Info("unregistered")

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

func handleDeleteError(err error, logger lager.Logger) error {
	if err == registry.ErrContainerNotFound {
		logger.Error("container-not-found", err)
		return api.ErrContainerNotFound
	}

	logger.Error("failed-to-delete-container", err)

	return err
}
