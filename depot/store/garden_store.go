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
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

var (
	ErrContainerNotFound = errors.New("container not found")
)

type GardenStore struct {
	gardenClient       garden.Client
	exchanger          Exchanger
	containerOwnerName string

	healthyMonitoringInterval   time.Duration
	unhealthyMonitoringInterval time.Duration

	transformer  *transformer.Transformer
	timeProvider timeprovider.TimeProvider

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
	gardenClient garden.Client,
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	healthyMonitoringInterval time.Duration,
	unhealthyMonitoringInterval time.Duration,
	transformer *transformer.Transformer,
	timeProvider timeprovider.TimeProvider,
	tracker InitializedTracker,
	eventEmitter EventEmitter,
) *GardenStore {
	return &GardenStore{
		gardenClient:       gardenClient,
		exchanger:          NewExchanger(containerOwnerName, containerMaxCPUShares, containerInodeLimit),
		containerOwnerName: containerOwnerName,

		healthyMonitoringInterval:   healthyMonitoringInterval,
		unhealthyMonitoringInterval: unhealthyMonitoringInterval,

		transformer:  transformer,
		timeProvider: timeProvider,

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
			continue
		}

		result = append(result, container)
	}

	return result, nil
}

func (store *GardenStore) Create(container executor.Container) (executor.Container, error) {
	if container.State != executor.StateInitializing {
		return executor.Container{}, executor.ErrInvalidTransition
	}
	container.State = executor.StateCreated

	container, err := store.exchanger.CreateInGarden(store.gardenClient, container)
	if err != nil {
		return executor.Container{}, err
	}

	store.tracker.Initialize(container)

	return container, nil
}

func (store *GardenStore) freeStepProcess(guid string) (ifrit.Process, bool) {
	store.processesL.Lock()
	process, found := store.runningProcesses[guid]
	delete(store.runningProcesses, guid)
	store.processesL.Unlock()

	if !found {
		return nil, false
	}

	process.Signal(os.Interrupt)
	return process, true
}

func (store *GardenStore) Stop(logger lager.Logger, guid string) error {
	process, found := store.freeStepProcess(guid)
	if !found {
		return ErrContainerNotFound
	}

	<-process.Wait()

	return nil
}

func (store *GardenStore) Destroy(logger lager.Logger, guid string) error {
	store.freeStepProcess(guid)

	err := store.gardenClient.Destroy(guid)
	if err != nil {
		return err
	}

	store.tracker.Deinitialize(guid)

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

func (store *GardenStore) Run(logger lager.Logger, container executor.Container) error {
	logger = logger.WithData(lager.Data{
		"container": container.Guid,
	})

	if container.State != executor.StateCreated {
		return executor.ErrInvalidTransition
	}

	gardenContainer, err := store.gardenClient.Lookup(container.Guid)
	if err != nil {
		logger.Debug("container-deleted-just-before-running")
		return err
	}

	logStreamer := log_streamer.New(
		container.Log.Guid,
		container.Log.SourceName,
		container.Log.Index,
	)

	seq := []steps.Step{}

	if container.Setup != nil {
		seq = append(seq, store.transformer.StepFor(
			logStreamer,
			container.Setup,
			gardenContainer,
			container.ExternalIP,
			container.Ports,
			logger,
		))
	}

	parallelSequence := []steps.Step{
		store.transformer.StepFor(
			logStreamer,
			container.Action,
			gardenContainer,
			container.ExternalIP,
			container.Ports,
			logger,
		),
	}

	hasStartedRunning := make(chan struct{}, 1)

	var monitorStep steps.Step

	if container.Monitor != nil {
		monitoredStep := store.transformer.StepFor(
			logStreamer,
			container.Monitor,
			gardenContainer,
			container.ExternalIP,
			container.Ports,
			logger,
		)

		monitorStep = steps.NewMonitor(
			monitoredStep,
			hasStartedRunning,
			logger.Session("monitor"),
			store.timeProvider,
			time.Duration(container.StartTimeout)*time.Second,
			store.healthyMonitoringInterval,
			store.unhealthyMonitoringInterval,
		)
		parallelSequence = append(parallelSequence, monitorStep)
	} else {
		// this container isn't monitored, so we mark it running right away
		hasStartedRunning <- struct{}{}
	}

	seq = append(seq, steps.NewCodependent(parallelSequence))

	step := steps.NewSerial(seq)

	store.runStepProcess(logger, step, monitorStep, hasStartedRunning, gardenContainer, container.Guid)

	return nil
}

func (store *GardenStore) runStepProcess(
	logger lager.Logger,
	step steps.Step,
	monitorStep steps.Step,
	hasStartedRunning <-chan struct{},
	gardenContainer garden.Container,
	guid string,
) {
	process := ifrit.Invoke(ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		seqComplete := make(chan error)

		close(ready)

		go func() {
			seqComplete <- step.Perform()
		}()

		result := executor.ContainerRunResult{}

	OUTER_LOOP:
		for {
			select {
			case <-signals:
				signals = nil
				if monitorStep != nil {
					monitorStep.Cancel()
				}
				step.Cancel()

			case <-hasStartedRunning:
				hasStartedRunning = nil
				err := store.transitionToRunning(gardenContainer)
				if err != nil {
					logger.Error("failed-to-transition-to-running", err)
					result.Failed = true
					result.FailureReason = err.Error()
					break OUTER_LOOP
				}

			case err := <-seqComplete:
				if err != nil {
					result.Failed = true
					result.FailureReason = err.Error()
				}
				break OUTER_LOOP
			}
		}

		err := store.transitionToComplete(gardenContainer, result)

		if err != nil {
			logger.Error("failed-to-transition-to-complete", err)
		}

		return nil
	}))

	store.processesL.Lock()
	store.runningProcesses[guid] = process
	store.processesL.Unlock()
}

func (store *GardenStore) transitionToRunning(gardenContainer garden.Container) error {
	err := gardenContainer.SetProperty(ContainerStateProperty, string(executor.StateRunning))
	if err != nil {
		return err
	}

	executorContainer, err := store.exchanger.Garden2Executor(gardenContainer)
	if err != nil {
		return err
	}

	store.eventEmitter.EmitEvent(executor.ContainerRunningEvent{
		Container: executorContainer,
	})

	return nil
}

func (store *GardenStore) transitionToComplete(gardenContainer garden.Container, result executor.ContainerRunResult) error {
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

func (store *GardenStore) TrackContainers(interval time.Duration, logger lager.Logger) ifrit.Runner {
	logger = logger.Session("garden-store-track-containers")

	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		ticker := store.timeProvider.NewTicker(interval)
		defer ticker.Stop()

		close(ready)

	dance:
		for {
			select {
			case <-signals:
				break dance

			case <-ticker.C():
				containers, err := store.List(nil)
				if err != nil {
					logger.Error("failed-to-list-containers", err)
					continue
				}

				store.tracker.SyncInitialized(containers)
			}
		}

		return nil
	})
}
