package depot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/allocations"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/registry"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	garden "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

const ContainerInitializationFailedMessage = "failed to initialize container: %s"

type client struct {
	exchanger        exchanger.Exchanger
	gardenClient     garden.Client
	allocationClient garden.Client
	logEmitter       logemitter.Emitter
	registry         registry.Registry
	transformer      *transformer.Transformer
	logger           lager.Logger
}

func NewClient(
	exchanger exchanger.Exchanger,
	registry registry.Registry,
	gardenClient garden.Client,
	allocationClient garden.Client,
	logEmitter logemitter.Emitter,
	transformer *transformer.Transformer,
	logger lager.Logger,
) executor.Client {
	return &client{
		exchanger:        exchanger,
		gardenClient:     gardenClient,
		allocationClient: allocationClient,
		registry:         registry,
		logEmitter:       logEmitter,
		transformer:      transformer,
		logger:           logger.Session("depot-client"),
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

	err := c.registry.Reserve(executorContainer)
	if err != nil {
		logger.Error("full", err)
		return executor.Container{}, executor.ErrInsufficientResourcesAvailable
	}

	executorContainer.State = executor.StateReserved
	executorContainer.AllocatedAt = time.Now().UnixNano()

	_, err = c.exchanger.Executor2Garden(c.allocationClient, executorContainer)
	if err == allocations.ErrContainerAlreadyExists {
		logger.Error("container-already-allocated", err)
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	return executorContainer, nil
}

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	gardenContainer, err := c.allocationClient.Lookup(guid)
	if err != nil {
		gardenContainer, err = c.gardenClient.Lookup(guid)
		if err != nil {
			logger.Error("container-not-found", err)
			return executor.Container{}, executor.ErrContainerNotFound
		}
	}

	return c.exchanger.Garden2Executor(gardenContainer)
}

func (c *client) RunContainer(guid string) error {
	logger := c.logger.Session("run", lager.Data{
		"guid": guid,
	})

	allocatedContainer, err := c.allocationClient.Lookup(guid)
	if err != nil {
		logger.Error("failed-to-find-container", err)
		return executor.ErrContainerNotFound
	}

	executorContainer, err := c.exchanger.Garden2Executor(allocatedContainer)
	if err != nil {
		logger.Error("failed-to-read-container", err)
		return err
	}

	err = allocatedContainer.SetProperty(exchanger.ContainerStateProperty, string(executor.StateInitializing))
	if err != nil {
		logger.Error("failed-to-initialize-registry-container", err)
		return err
	}

	go func() {
		var gardenContainer garden.Container
		var err error

		defer func() {
			if err != nil {
				if gardenContainer != nil {
					logger.Error("destroying-container-after-failed-init", err)
					destroyErr := c.gardenClient.Destroy(gardenContainer.Handle())
					if destroyErr != nil {
						logger.Error("destroying-container-after-failed-init-also-failed", destroyErr)
					}
				}

				result := executor.ContainerRunResult{
					Failed:        true,
					FailureReason: fmt.Sprintf(ContainerInitializationFailedMessage, err.Error()),
				}

				resultPayload, err := json.Marshal(result)
				if err != nil {
					logger.Error("failed-to-marshal-result", err)
					return
				}

				err = allocatedContainer.SetProperty(exchanger.ContainerResultProperty, string(resultPayload))
				if err != nil {
					logger.Error("failed-to-set-result", err)
					return
				}

				err = allocatedContainer.SetProperty(exchanger.ContainerStateProperty, string(executor.StateCompleted))
				if err != nil {
					logger.Error("failed-to-set-state", err)
					return
				}
			}
		}()

		executorContainer.State = executor.StateCreated

		gardenContainer, err = c.exchanger.Executor2Garden(c.gardenClient, executorContainer)
		if err != nil {
			logger.Error("failed-to-create-container", err)
			return
		}

		info, err := gardenContainer.Info()
		log.Println(info.Properties, err)

		err = c.allocationClient.Destroy(guid)
		if err != nil {
			logger.Error("failed-to-remove-allocated-container", err)
			return
		}

		logger = logger.WithData(lager.Data{
			"handle": gardenContainer.Handle(),
		})

		var result string
		logStreamer := log_streamer.New(
			executorContainer.Log.Guid,
			executorContainer.Log.SourceName,
			executorContainer.Log.Index,
			c.logEmitter,
		)

		steps, err := c.transformer.StepsFor(
			logStreamer,
			executorContainer.Actions,
			executorContainer.Env,
			gardenContainer,
			&result,
		)
		if err != nil {
			logger.Error("steps-invalid", err)
			err = executor.ErrStepsInvalid
			return
		}

		run := RunSequence{
			Container:    gardenContainer,
			CompleteURL:  executorContainer.CompleteURL,
			Registration: executorContainer,
			Sequence:     sequence.New(steps),
			Result:       &result,
			Logger:       c.logger,
		}

		process := ifrit.Invoke(run)

		c.registry.Start(run.Registration.Guid, process)
		logger.Info("started")
	}()

	return nil
}

func (c *client) ListContainers() ([]executor.Container, error) {
	containersByHandle := make(map[string]garden.Container)

	gardenContainers, err := c.gardenClient.Containers(garden.Properties{})
	if err != nil {
		return nil, err
	}

	allocatedContainers, err := c.allocationClient.Containers(garden.Properties{})
	if err != nil {
		return nil, err
	}

	for _, gardenContainer := range gardenContainers {
		containersByHandle[gardenContainer.Handle()] = gardenContainer
	}

	for _, gardenContainer := range allocatedContainers {
		containersByHandle[gardenContainer.Handle()] = gardenContainer
	}

	var containers []executor.Container
	for _, gardenContainer := range containersByHandle {
		executorContainer, err := c.exchanger.Garden2Executor(gardenContainer)
		if err != nil {
			return nil, err
		}

		containers = append(containers, executorContainer)
	}

	return containers, nil
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete", lager.Data{
		"guid": guid,
	})

	var container garden.Container
	var client garden.Client
	var err error

	if container, err = c.allocationClient.Lookup(guid); err == nil {
		client = c.allocationClient
	} else if container, err = c.gardenClient.Lookup(guid); err == nil {
		client = c.gardenClient
	} else {
		return executor.ErrContainerNotFound
	}

	executorContainer, err := c.exchanger.Garden2Executor(container)
	if err != nil {
		logger.Error("failed-to-convert-container", err)
		return err
	}

	err = client.Destroy(guid)
	if err != nil {
		return err
	}

	c.registry.Delete(executorContainer)

	return nil
}

func (c *client) RemainingResources() (executor.ExecutorResources, error) {
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

	container, err := c.gardenClient.Lookup(guid)
	if err != nil {
		logger.Error("lookup-failed", err)
		return nil, err
	}

	return container.StreamOut(sourcePath)
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
