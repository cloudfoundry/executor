package depot

import (
	"fmt"
	"io"
	"os"

	"github.com/cloudfoundry-incubator/executor"
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
	exchanger    exchanger.Exchanger
	gardenClient garden.Client
	registry     registry.Registry
	logEmitter   logemitter.Emitter
	transformer  *transformer.Transformer
	logger       lager.Logger
}

func NewClient(
	exchanger exchanger.Exchanger,
	gardenClient garden.Client,
	registry registry.Registry,
	logEmitter logemitter.Emitter,
	transformer *transformer.Transformer,
	logger lager.Logger,
) executor.Client {
	return &client{
		exchanger:    exchanger,
		gardenClient: gardenClient,
		registry:     registry,
		logEmitter:   logEmitter,
		transformer:  transformer,
		logger:       logger.Session("depot-client"),
	}
}

func (c *client) AllocateContainer(guid string, request executor.Container) (executor.Container, error) {
	if request.CPUWeight > 100 || request.CPUWeight < 0 {
		return executor.Container{}, executor.ErrLimitsInvalid
	} else if request.CPUWeight == 0 {
		request.CPUWeight = 100
	}

	logger := c.logger.Session("allocate", lager.Data{
		"guid": guid,
	})

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

func (c *client) GetContainer(guid string) (executor.Container, error) {
	logger := c.logger.Session("get", lager.Data{
		"guid": guid,
	})

	executorContainer, err := c.registry.FindByGuid(guid)

	if err != nil {
		logger.Error("container-not-found", err)
		return executor.Container{}, executor.ErrContainerNotFound
	}

	if executorContainer.ContainerHandle != "" {
		gardenContainer, err := c.gardenClient.Lookup(executorContainer.ContainerHandle)
		if err != nil {
			logger.Error("lookup-failed", err)
			return executor.Container{}, err
		}

		executorContainer, err = c.exchanger.Garden2Executor(c.gardenClient, gardenContainer)
		if err != nil {
			logger.Error("presenting-container-failed", err)
			return executor.Container{}, err
		}
	}

	return executorContainer, nil
}

func (c *client) RunContainer(guid string) error {
	logger := c.logger.Session("run", lager.Data{
		"guid": guid,
	})

	container, err := c.registry.FindByGuid(guid)
	if err != nil {
		logger.Error("failed-to-find-container", err)
		return executor.ErrContainerNotFound
	}

	container, err = c.registry.Initialize(guid)
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

				completeErr := c.registry.Complete(guid, executor.ContainerRunResult{
					Failed:        true,
					FailureReason: fmt.Sprintf(ContainerInitializationFailedMessage, err.Error()),
				})
				if completeErr != nil {
					logger.Error("completing-container-while-init-also-failed", completeErr)
				}
			}
		}()

		// container.State = executor.StateCreated

		gardenContainer, err = c.exchanger.Executor2Garden(c.gardenClient, container)
		if err != nil {
			logger.Error("failed-to-create-container", err)
			return
		}

		container, err = c.registry.Create(guid, gardenContainer.Handle())
		if err != nil {
			logger.Error("failed-to-register-container", err, lager.Data{
				"container-handle": container.ContainerHandle,
			})
			return
		}

		logger = logger.WithData(lager.Data{
			"handle": gardenContainer.Handle(),
		})

		var result string
		logStreamer := log_streamer.New(container.Log.Guid, container.Log.SourceName, container.Log.Index, c.logEmitter)

		steps, err := c.transformer.StepsFor(logStreamer, container.Actions, container.Env, gardenContainer, &result)
		if err != nil {
			logger.Error("steps-invalid", err)
			err = executor.ErrStepsInvalid
			return
		}

		run := RunSequence{
			Container:    gardenContainer,
			CompleteURL:  container.CompleteURL,
			Registration: container,
			Sequence:     sequence.New(steps),
			Result:       &result,
			Registry:     c.registry,
			Logger:       c.logger,
		}

		process := ifrit.Invoke(run)

		c.registry.Start(run.Registration.Guid, process)

		logger.Info("started")
	}()

	return nil
}

func (c *client) ListContainers() ([]executor.Container, error) {
	return c.registry.GetAllContainers(), nil
}

func (c *client) DeleteContainer(guid string) error {
	logger := c.logger.Session("delete", lager.Data{
		"guid": guid,
	})

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
