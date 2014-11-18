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
	"github.com/cloudfoundry-incubator/executor/depot/steps"
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

	gardenClient       garden.Client
	exchanger          Exchanger
	containerOwnerName string

	healthyMonitoringInterval   time.Duration
	unhealthyMonitoringInterval time.Duration

	logEmitter  logemitter.Emitter
	transformer *transformer.Transformer
	timer       timer.Timer

	tracker InitializedTracker

	eventEmitter EventEmitter

	runningProcesses map[string]ifrit.Process
	processesL       sync.Mutex
}

type InitializedTracker interface {
	Initialize(executor.Container)
	Deinitialize(string)
	SyncInitialized([]executor.Container)
}

func NewGardenStore(
	logger lager.Logger,
	gardenClient garden.Client,
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	healthyMonitoringInterval time.Duration,
	unhealthyMonitoringInterval time.Duration,
	logEmitter logemitter.Emitter,
	transformer *transformer.Transformer,
	timer timer.Timer,
	tracker InitializedTracker,
	eventEmitter EventEmitter,
) *GardenStore {
	return &GardenStore{
		logger: logger,

		gardenClient:       gardenClient,
		exchanger:          NewExchanger(containerOwnerName, containerMaxCPUShares, containerInodeLimit),
		containerOwnerName: containerOwnerName,

		healthyMonitoringInterval:   healthyMonitoringInterval,
		unhealthyMonitoringInterval: unhealthyMonitoringInterval,

		logEmitter:  logEmitter,
		transformer: transformer,
		timer:       timer,

		tracker: tracker,

		eventEmitter: eventEmitter,

		runningProcesses: map[string]ifrit.Process{},
	}
}

func (store *GardenStore) Lookup(guid string) (executor.Container, error) {
	gardenContainer, err := store.gardenClient.Lookup(guid)
	if err != nil {
		return executor.Container{}, ErrContainerNotFound
	}

	return store.exchanger.Garden2Executor(gardenContainer)
}

func (store *GardenStore) List(tags executor.Tags) ([]executor.Container, error) {
	filter := garden.Properties{
		ContainerOwnerProperty: store.containerOwnerName,
	}

	for k, v := range tags {
		filter[tagPropertyPrefix+k] = v
	}

	gardenContainers, err := store.gardenClient.Containers(filter)
	if err != nil {
		return nil, ErrContainerNotFound
	}

	result := make([]executor.Container, 0, len(gardenContainers))

	for _, gardenContainer := range gardenContainers {
		container, err := store.exchanger.Garden2Executor(gardenContainer)
		if err != nil {
			store.logger.Error("failed-to-get-container-info", err, lager.Data{
				"handle": gardenContainer.Handle(),
			})

			continue
		}

		result = append(result, container)
	}

	return result, nil
}

func (store *GardenStore) Create(container executor.Container) (executor.Container, error) {
	container.State = executor.StateCreated

	_, err := store.exchanger.Executor2Garden(store.gardenClient, container)
	if err != nil {
		return executor.Container{}, err
	}

	store.tracker.Initialize(container)

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

	err := store.gardenClient.Destroy(guid)
	if err != nil {
		return err
	}

	store.tracker.Deinitialize(guid)

	return nil
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

	executorContainer, err := store.exchanger.Garden2Executor(gardenContainer)
	if err != nil {
		return err
	}

	store.eventEmitter.EmitEvent(executor.ContainerCompleteEvent{
		Container: executorContainer,
	})

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

	seq := []steps.Step{}

	if container.Setup != nil {
		seq = append(seq, store.transformer.StepFor(
			logStreamer,
			container.Setup,
			gardenContainer,
		))
	}

	parallelSequence := []steps.Step{
		store.transformer.StepFor(
			logStreamer,
			container.Action,
			gardenContainer,
		),
	}

	monitorEvents := make(chan steps.HealthEvent)

	if container.Monitor != nil {
		monitoredStep := store.transformer.StepFor(
			logStreamer,
			container.Monitor,
			gardenContainer,
		)

		parallelSequence = append(parallelSequence, steps.NewMonitor(
			monitoredStep,
			monitorEvents,
			store.logger.Session("monitor"),
			timer.NewTimer(),
			store.healthyMonitoringInterval,
			store.unhealthyMonitoringInterval,
		))
	}

	seq = append(seq, steps.NewCodependent(parallelSequence))

	step := steps.NewSerial(seq)

	process := ifrit.Invoke(ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		seqComplete := make(chan error)

		close(ready)

		go func() {
			seqComplete <- step.Perform()
		}()

		for {
			select {
			case <-signals:
				signals = nil
				step.Cancel()

			case healthEvent := <-monitorEvents:
				err := updateHealth(store, container, gardenContainer, healthEvent)
				if err != nil {
					return err
				}

			case seqErr := <-seqComplete:
				result := executor.ContainerRunResult{}

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

func updateHealth(store *GardenStore, executorContainer executor.Container, gardenContainer garden.Container, healthEvent steps.HealthEvent) error {
	var health executor.Health

	if healthEvent == steps.Healthy {
		health = executor.HealthUp
	} else {
		health = executor.HealthDown
	}

	err := gardenContainer.SetProperty(ContainerHealthProperty, string(health))
	if err != nil {
		return err
	}

	store.eventEmitter.EmitEvent(executor.ContainerHealthEvent{
		Container: executorContainer,
		Health:    health,
	})

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
				containers, err := store.List(nil)
				if err != nil {
					store.logger.Error("failed-to-list-containers", err)
					continue
				}

				store.tracker.SyncInitialized(containers)
			}
		}

		return nil
	})
}
