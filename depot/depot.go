package depot

import (
	"io"
	"os"
	"strings"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/registry"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	gapi "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type client struct {
	containerOwnerName    string
	containerMaxCPUShares uint64
	containerInodeLimit   uint64
	gardenClient          gapi.Client
	registry              registry.Registry
	logEmitter            logemitter.Emitter
	transformer           *transformer.Transformer
	logger                lager.Logger
}

func NewClient(
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	gardenClient gapi.Client,
	registry registry.Registry,
	logEmitter logemitter.Emitter,
	transformer *transformer.Transformer,
	logger lager.Logger,
) executor.Client {
	return &client{
		containerOwnerName:    containerOwnerName,
		containerMaxCPUShares: containerMaxCPUShares,
		containerInodeLimit:   containerInodeLimit,
		gardenClient:          gardenClient,
		registry:              registry,
		logEmitter:            logEmitter,
		transformer:           transformer,
		logger:                logger.Session("depot-client"),
	}
}

func (c *client) AllocateContainer(guid string, request executor.ContainerAllocationRequest) (executor.Container, error) {
	if request.CPUWeight > 100 || request.CPUWeight < 0 {
		return executor.Container{}, executor.ErrLimitsInvalid
	} else if request.CPUWeight == 0 {
		request.CPUWeight = 100
	}

	logger := c.logger.Session("allocate", lager.Data{
		"guid": guid,
	})

	err := c.syncRegistry()
	if err != nil {
		return executor.Container{}, handleSyncErr(err, logger)
	}

	container, err := c.registry.Reserve(guid, request)
	if err == registry.ErrContainerAlreadyExists {
		logger.Error("container-already-allocated", err)
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	if err != nil {
		logger.Error("full", err)
		return executor.Container{}, executor.ErrInsufficientResourcesAvailable
	}

	return container, nil
}

func (c *client) InitializeContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("initialize", lager.Data{
		"guid": guid,
	})

	container, err := c.registry.FindByGuid(guid)
	if err != nil {
		logger.Error("failed-to-find-container", err)
		return executor.Container{}, executor.ErrContainerNotFound
	}

	container, err = c.registry.Initialize(guid)
	if err != nil {
		logger.Error("failed-to-initialize-registry-container", err)
		return executor.Container{}, err
	}

	properties := gapi.Properties{
		"executor:owner": c.containerOwnerName,
	}

	for k, v := range container.Tags {
		properties[tagPropertyPrefix+k] = v
	}

	containerClient, err := c.gardenClient.Create(gapi.ContainerSpec{
		RootFSPath: container.RootFSPath,
		Properties: properties,
	})
	if err != nil {
		logger.Error("failed-to-create-container", err)
		return executor.Container{}, err
	}

	defer func() {
		if err != nil {
			logger.Error("destroying-container-after-failed-init", err)
			destroyErr := c.gardenClient.Destroy(containerClient.Handle())
			if destroyErr != nil {
				logger.Error("destroying-container-after-failed-init-also-failed", destroyErr)
			}
		}
	}()

	err = c.limitContainerDiskAndMemory(container, containerClient)
	if err != nil {
		logger.Error("failed-to-limit-memory-and-disk", err)
		return executor.Container{}, err
	}

	err = c.limitContainerCPU(container, containerClient)
	if err != nil {
		logger.Error("failed-to-limit-cpu", err)
		return executor.Container{}, err
	}

	portMapping, err := c.mapPorts(container, containerClient)
	if err != nil {
		logger.Error("failed-to-map-ports", err)
		return executor.Container{}, err
	}

	container, err = c.registry.Create(guid, containerClient.Handle(), portMapping)
	if err != nil {
		logger.Error("failed-to-register-container", err, lager.Data{
			"container-handle": container.ContainerHandle,
		})
		return executor.Container{}, err
	}

	return container, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	err := c.syncRegistry()
	if err != nil {
		return executor.Container{}, handleSyncErr(err, logger)
	}

	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		logger.Error("container-not-found", err)
		return executor.Container{}, executor.ErrContainerNotFound
	}

	if registration.ContainerHandle != "" {
		container, err := c.gardenClient.Lookup(registration.ContainerHandle)
		if err != nil {
			logger.Error("lookup-failed", err)
			return executor.Container{}, err
		}

		if value, err := container.GetProperty(runResultFailedProperty); err == nil {
			registration.RunResult.Failed = value == runResultTrueValue

			if registration.RunResult.Failed {
				if value, err := container.GetProperty(runResultFailureReasonProperty); err == nil {
					registration.RunResult.FailureReason = value
				}
			}
		}

		info, err := container.Info()
		if err != nil {
			panic("TESTME")
			logger.Error("info-failed", err)
			return executor.Container{}, err
		}

		tags := executor.Tags{}
		for k, v := range info.Properties {
			if !strings.HasPrefix(k, tagPropertyPrefix) {
				continue
			}

			tags[k[len(tagPropertyPrefix):]] = v
		}

		registration.Tags = tags
	}

	return registration, nil
}

func (c *client) Run(guid string, request executor.ContainerRunRequest) error {
	logger := c.logger.Session("run", lager.Data{
		"guid": guid,
	})

	err := c.syncRegistry()
	if err != nil {
		return handleSyncErr(err, logger)
	}

	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		logger.Error("container-not-found", err)
		return executor.ErrContainerNotFound
	}

	logger = logger.WithData(lager.Data{
		"handle": registration.ContainerHandle,
	})

	container, err := c.gardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		logger.Error("lookup-failed", err)
		return err
	}

	var result string
	logStreamer := log_streamer.New(registration.Log.Guid, registration.Log.SourceName, registration.Log.Index, c.logEmitter)
	steps, err := c.transformer.StepsFor(logStreamer, request.Actions, request.Env, container, &result)
	if err != nil {
		logger.Error("steps-invalid", err)
		return executor.ErrStepsInvalid
	}

	run := RunSequence{
		Container:    container,
		CompleteURL:  request.CompleteURL,
		Registration: registration,
		Sequence:     sequence.New(steps),
		Result:       &result,
		Registry:     c.registry,
		Logger:       c.logger,
	}

	process := ifrit.Invoke(run)

	c.registry.Start(run.Registration.Guid, process)

	logger.Info("started")

	return nil
}

func (c *client) ListContainers() ([]executor.Container, error) {
	logger := c.logger.Session("list")
	err := c.syncRegistry()
	if err != nil {
		return []executor.Container{}, handleSyncErr(err, logger)
	}
	return c.registry.GetAllContainers(), nil
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete", lager.Data{
		"guid": guid,
	})

	err := c.syncRegistry()
	if err != nil {
		return handleSyncErr(err, logger)
	}

	reg, err := c.registry.FindByGuid(guid)
	if err != nil {
		return handleDeleteError(err, logger)
	}

	logger = logger.WithData(lager.Data{"handle": reg.ContainerHandle})
	logger.Debug("deleting")

	if reg.Process != nil {
		logger.Debug("interrupting")

		reg.Process.Signal(os.Interrupt)
		<-reg.Process.Wait()

		logger.Info("interrupted")
	}

	if reg.ContainerHandle != "" {
		logger.Debug("destroying")

		err = c.gardenClient.Destroy(reg.ContainerHandle)
		if err != nil {
			return handleDeleteError(err, logger)
		}

		logger.Info("destroyed")
	}

	logger.Debug("unregistering")

	err = c.registry.Delete(guid)
	if err != nil {
		return handleDeleteError(err, logger)
	}

	logger.Info("unregistered")

	return nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
	logger := c.logger.Session("remaining-resources")

	err := c.syncRegistry()
	if err != nil {
		logger.Error("could-not-sync-registry", err)
		return executor.ExecutorResources{}, err
	}

	cap := c.registry.CurrentCapacity()

	return executor.ExecutorResources{
		MemoryMB:   cap.MemoryMB,
		DiskMB:     cap.DiskMB,
		Containers: cap.Containers,
	}, nil
}

func (c *client) Ping() error {
	return c.gardenClient.Ping()
}

func (c *client) TotalResources() (executor.ExecutorResources, error) {
	totalCapacity := c.registry.TotalCapacity()

	resources := executor.ExecutorResources{
		MemoryMB:   totalCapacity.MemoryMB,
		DiskMB:     totalCapacity.DiskMB,
		Containers: totalCapacity.Containers,
	}
	return resources, nil
}

func (c *client) GetFiles(guid, sourcePath string) (io.ReadCloser, error) {
	logger := c.logger.Session("get-files", lager.Data{
		"guid":        guid,
		"source-path": sourcePath,
	})

	err := c.syncRegistry()
	if err != nil {
		return nil, handleSyncErr(err, logger)
	}

	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		logger.Error("container-not-found", err)
		return nil, executor.ErrContainerNotFound
	}

	logger = logger.WithData(lager.Data{
		"handle": registration.ContainerHandle,
	})

	container, err := c.gardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		logger.Error("lookup-failed", err)
		return nil, err
	}

	return container.StreamOut(sourcePath)
}

func (c *client) syncRegistry() error {
	containers, err := c.gardenClient.Containers(nil)
	if err != nil {
		return err
	}

	handleSet := make(map[string]struct{})
	for _, container := range containers {
		handleSet[container.Handle()] = struct{}{}
	}

	c.registry.Sync(handleSet)
	return nil
}

func (c *client) limitContainerDiskAndMemory(reg executor.Container, containerClient gapi.Container) error {
	if reg.MemoryMB != 0 {
		err := containerClient.LimitMemory(gapi.MemoryLimits{
			LimitInBytes: uint64(reg.MemoryMB * 1024 * 1024),
		})
		if err != nil {
			return err
		}
	}

	err := containerClient.LimitDisk(gapi.DiskLimits{
		ByteHard:  uint64(reg.DiskMB * 1024 * 1024),
		InodeHard: c.containerInodeLimit,
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *client) limitContainerCPU(reg executor.Container, containerClient gapi.Container) error {
	if reg.CPUWeight != 0 {
		err := containerClient.LimitCPU(gapi.CPULimits{
			LimitInShares: uint64(float64(c.containerMaxCPUShares) * float64(reg.CPUWeight) / 100.0),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *client) mapPorts(reg executor.Container, containerClient gapi.Container) ([]executor.PortMapping, error) {
	var result []executor.PortMapping
	for _, mapping := range reg.Ports {
		hostPort, containerPort, err := containerClient.NetIn(mapping.HostPort, mapping.ContainerPort)
		if err != nil {
			return nil, err
		}

		result = append(result, executor.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
		})
	}

	return result, nil
}

func handleSyncErr(err error, logger lager.Logger) error {
	logger.Error("could-not-sync-registry", err)
	return err
}

func handleDeleteError(err error, logger lager.Logger) error {
	if err == registry.ErrContainerNotFound {
		logger.Error("container-not-found", err)
		return executor.ErrContainerNotFound
	}

	logger.Error("failed-to-delete-container", err)

	return err
}
