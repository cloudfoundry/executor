package store

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	garden "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/timer"
	"github.com/tedsuo/ifrit"
)

var (
	ErrContainerNotFound = errors.New("container not found")
)

type GardenStore struct {
	logger lager.Logger

	gardenClient          garden.Client
	containerOwnerName    string
	containerMaxCPUShares uint64
	containerInodeLimit   uint64

	logEmitter  logemitter.Emitter
	transformer *transformer.Transformer
	timer       timer.Timer

	containers   uint64
	usedMemoryMB uint64
	usedDiskMB   uint64
	resourcesL   sync.RWMutex

	runningProcesses map[string]ifrit.Process
	processesL       sync.Mutex
}

func NewGardenStore(
	logger lager.Logger,
	gardenClient garden.Client,
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	logEmitter logemitter.Emitter,
	transformer *transformer.Transformer,
	timer timer.Timer,
) *GardenStore {
	return &GardenStore{
		logger: logger,

		gardenClient:          gardenClient,
		containerInodeLimit:   containerInodeLimit,
		containerMaxCPUShares: containerMaxCPUShares,
		containerOwnerName:    containerOwnerName,
		logEmitter:            logEmitter,
		transformer:           transformer,
		timer:                 timer,

		runningProcesses: map[string]ifrit.Process{},
	}
}

func (store *GardenStore) Lookup(guid string) (executor.Container, error) {
	gardenContainer, err := store.gardenClient.Lookup(guid)
	if err != nil {
		return executor.Container{}, ErrContainerNotFound
	}

	exchanger := NewExchanger(store.containerOwnerName, store.containerMaxCPUShares, store.containerInodeLimit)
	return exchanger.Garden2Executor(gardenContainer)
}

func (store *GardenStore) List() ([]executor.Container, error) {
	gardenContainers, err := store.gardenClient.Containers(garden.Properties{
		ContainerOwnerProperty: store.containerOwnerName,
	})
	if err != nil {
		return nil, ErrContainerNotFound
	}

	exchanger := NewExchanger(store.containerOwnerName, store.containerMaxCPUShares, store.containerInodeLimit)
	result := make([]executor.Container, 0, len(gardenContainers))

	for _, gardenContainer := range gardenContainers {
		container, err := exchanger.Garden2Executor(gardenContainer)
		if err != nil {
			return nil, err
		}

		result = append(result, container)
	}

	return result, nil
}

func (store *GardenStore) Create(container executor.Container) (executor.Container, error) {
	exchanger := NewExchanger(store.containerOwnerName, store.containerMaxCPUShares, store.containerInodeLimit)

	container.State = executor.StateCreated

	_, err := exchanger.Executor2Garden(store.gardenClient, container)
	if err != nil {
		return executor.Container{}, err
	}

	store.resourcesL.Lock()
	store.usedMemoryMB += uint64(container.MemoryMB)
	store.usedDiskMB += uint64(container.DiskMB)
	store.containers++
	store.resourcesL.Unlock()

	return container, nil
}

func (store *GardenStore) Destroy(guid string) error {
	store.processesL.Lock()
	process, found := store.runningProcesses[guid]
	delete(store.runningProcesses, guid)
	store.processesL.Unlock()

	if found {
		process.Signal(os.Interrupt)
		<-process.Wait()
	}

	container, err := store.gardenClient.Lookup(guid)
	if err != nil {
		return ErrContainerNotFound
	}

	exchanger := NewExchanger(store.containerOwnerName, store.containerMaxCPUShares, store.containerInodeLimit)
	executorContainer, err := exchanger.Garden2Executor(container)
	if err != nil {
		return err
	}

	err = store.gardenClient.Destroy(guid)
	if err != nil {
		return err
	}

	store.resourcesL.Lock()
	store.usedMemoryMB -= uint64(executorContainer.MemoryMB)
	store.usedDiskMB -= uint64(executorContainer.DiskMB)
	store.containers--
	store.resourcesL.Unlock()

	return nil
}

func (store *GardenStore) ConsumedResources() executor.ExecutorResources {
	store.resourcesL.RLock()
	defer store.resourcesL.RUnlock()

	return executor.ExecutorResources{
		MemoryMB:   int(store.usedMemoryMB),
		DiskMB:     int(store.usedDiskMB),
		Containers: int(store.containers),
	}
}

func (store *GardenStore) Complete(guid string, result executor.ContainerRunResult) error {
	gardenContainer, err := store.gardenClient.Lookup(guid)
	if err != nil {
		return ErrContainerNotFound
	}

	resultJson, err := json.Marshal(result)
	if err != nil {
		return err
	}

	err = gardenContainer.SetProperty(ContainerResultProperty, string(resultJson))
	if err != nil {
		return err
	}

	err = gardenContainer.SetProperty(ContainerStateProperty, string(executor.StateCompleted))
	if err != nil {
		return err
	}

	return nil
}

func (store *GardenStore) GetFiles(guid, sourcePath string) (io.ReadCloser, error) {
	container, err := store.gardenClient.Lookup(guid)
	if err != nil {
		return nil, ErrContainerNotFound
	}

	return container.StreamOut(sourcePath)
}

func (store *GardenStore) Ping() error {
	return store.gardenClient.Ping()
}

func (store *GardenStore) Run(container executor.Container, callback func(executor.ContainerRunResult)) error {
	gardenContainer, err := store.gardenClient.Lookup(container.Guid)
	if err != nil {
		return ErrContainerNotFound
	}

	logStreamer := log_streamer.New(
		container.Log.Guid,
		container.Log.SourceName,
		container.Log.Index,
		store.logEmitter,
	)

	steps, err := store.transformer.StepsFor(
		logStreamer,
		container.Actions,
		container.Env,
		gardenContainer,
	)

	if err != nil {
		return executor.ErrStepsInvalid
	}

	process := ifrit.Invoke(ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		seqComplete := make(chan error)
		seq := sequence.New(steps)

		go func() {
			seqComplete <- seq.Perform()
		}()

		close(ready)

		for {
			select {
			case <-signals:
				signals = nil
				seq.Cancel()

			case seqErr := <-seqComplete:
				if seqErr == sequence.CancelledError {
					// we do this because we don't want to hit the rep's callback
					// when it destroys the container. which is silly.
					//
					// remove when the callback is gone.
					return seqErr
				}

				result := executor.ContainerRunResult{
					Guid: container.Guid,
				}

				if seqErr == nil {
					result.Failed = false
				} else {
					result.Failed = true
					result.FailureReason = seqErr.Error()
				}

				callback(result)

				return nil
			}
		}
	}))

	store.processesL.Lock()
	store.runningProcesses[container.Guid] = process
	store.processesL.Unlock()

	return nil
}

func (store *GardenStore) TrackContainers(interval time.Duration) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		ticker := store.timer.Every(interval)
		close(ready)

	dance:
		for {
			select {
			case <-signals:
				break dance

			case <-ticker:
				containers, err := store.List()
				if err != nil {
					store.logger.Error("failed-to-list-containers", err)
					continue
				}

				store.resourcesL.Lock()

				store.usedDiskMB = 0
				store.usedMemoryMB = 0
				store.containers = 0

				for _, container := range containers {
					store.usedMemoryMB += uint64(container.MemoryMB)
					store.usedDiskMB += uint64(container.DiskMB)
					store.containers++
				}

				store.resourcesL.Unlock()
			}
		}

		return nil
	})
}
