package gardenstore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry/gunk/workpool"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type GardenStore struct {
	gardenClient       garden.Client
	exchanger          Exchanger
	containerOwnerName string

	healthyMonitoringInterval   time.Duration
	unhealthyMonitoringInterval time.Duration

	transformer *transformer.Transformer
	clock       clock.Clock

	eventEmitter EventEmitter

	runningProcesses map[string]ifrit.Process
	processesL       sync.Mutex

	workPool *workpool.WorkPool
}

func NewGardenStore(
	gardenClient garden.Client,
	containerOwnerName string,
	containerMaxCPUShares uint64,
	containerInodeLimit uint64,
	healthyMonitoringInterval time.Duration,
	unhealthyMonitoringInterval time.Duration,
	transformer *transformer.Transformer,
	clock clock.Clock,
	eventEmitter EventEmitter,
) *GardenStore {
	return &GardenStore{
		gardenClient:       gardenClient,
		exchanger:          NewExchanger(containerOwnerName, containerMaxCPUShares, containerInodeLimit),
		containerOwnerName: containerOwnerName,

		healthyMonitoringInterval:   healthyMonitoringInterval,
		unhealthyMonitoringInterval: unhealthyMonitoringInterval,

		transformer: transformer,
		clock:       clock,

		eventEmitter: eventEmitter,

		runningProcesses: map[string]ifrit.Process{},

		workPool: workpool.NewWorkPool(32),
	}
}

func (store *GardenStore) Lookup(logger lager.Logger, guid string) (executor.Container, error) {
	gardenContainer, err := store.lookup(logger, guid)
	if err != nil {
		return executor.Container{}, err
	}

	return store.exchanger.Info(logger, gardenContainer)
}

func (store *GardenStore) lookup(logger lager.Logger, guid string) (garden.Container, error) {
	gardenContainer, err := store.gardenClient.Lookup(guid)
	if err != nil {
		if _, ok := err.(garden.ContainerNotFoundError); ok {
			logger.Info("lookup-failed-container-not-found", lager.Data{"guid": guid})
			err = executor.ErrContainerNotFound
		} else {
			logger.Error("lookup-failed", err, lager.Data{"guid": guid})
		}

		return nil, err
	}

	return gardenContainer, err
}

func (store *GardenStore) List(logger lager.Logger, tags executor.Tags) ([]executor.Container, error) {
	filter := garden.Properties{
		ContainerOwnerProperty: store.containerOwnerName,
	}

	for k, v := range tags {
		filter[tagPropertyPrefix+k] = v
	}

	logger.Debug("listing-garden-containers", lager.Data{"filter": filter})
	gardenContainers, err := store.gardenClient.Containers(filter)
	if err != nil {
		logger.Error("failed-listing-garden-containers", err, lager.Data{"filter": filter})
		return nil, err
	}
	logger.Debug("succeeded-listing-garden-containers", lager.Data{"filter": filter})

	return store.exchanger.Infos(logger, store.gardenClient, gardenContainers)
}

func (store *GardenStore) Metrics(logger lager.Logger, containerGuids []string) (map[string]executor.ContainerMetrics, error) {
	gardenMetrics, err := store.gardenClient.BulkMetrics(containerGuids)
	if err != nil {
		return nil, err
	}

	containerMetrics := map[string]executor.ContainerMetrics{}
	for _, containerGuid := range containerGuids {
		if metrics, found := gardenMetrics[containerGuid]; found {
			if metrics.Err == nil {
				gmetrics := metrics.Metrics
				containerMetrics[containerGuid] = executor.ContainerMetrics{
					MemoryUsageInBytes: gmetrics.MemoryStat.TotalRss + (gmetrics.MemoryStat.TotalCache - gmetrics.MemoryStat.TotalInactiveFile),
					DiskUsageInBytes:   gmetrics.DiskStat.BytesUsed,
					TimeSpentInCPU:     time.Duration(gmetrics.CPUStat.Usage),
				}
			}
		}
	}

	return containerMetrics, nil
}

func (store *GardenStore) Create(logger lager.Logger, container executor.Container) (executor.Container, error) {
	if container.State != executor.StateInitializing {
		return executor.Container{}, executor.ErrInvalidTransition
	}
	container.State = executor.StateCreated

	logStreamer := log_streamer.New(
		container.LogConfig.Guid,
		container.LogConfig.SourceName,
		container.LogConfig.Index,
	)

	fmt.Fprintf(logStreamer.Stdout(), "Creating container\n")

	container, err := store.exchanger.CreateInGarden(logger, store.gardenClient, container)
	if err != nil {
		fmt.Fprintf(logStreamer.Stderr(), "Failed to create container\n")
		return executor.Container{}, err
	}

	fmt.Fprintf(logStreamer.Stdout(), "Successfully created container\n")

	return container, nil
}

func (store *GardenStore) Stop(logger lager.Logger, guid string) error {
	logger = logger.Session("stop", lager.Data{"guid": guid})
	logger.Info("started")
	defer logger.Info("finished")

	process, found := store.freeStepProcess(logger, guid)
	if !found {
		return executor.ErrContainerNotFound
	}

	<-process.Wait()

	return nil
}

func (store *GardenStore) Destroy(logger lager.Logger, guid string) error {
	logger = logger.Session("destroy", lager.Data{"guid": guid})
	logger.Info("started")

	process, found := store.freeStepProcess(logger, guid)
	if found {
		<-process.Wait()
	}

	err := store.gardenClient.Destroy(guid)
	if err != nil {
		if _, ok := err.(garden.ContainerNotFoundError); ok {
			logger.Info("container-not-found")
			return nil
		}

		logger.Error("failed-to-destroy-garden-container", err)
		return err
	}

	logger.Info("succeeded")

	return nil
}

func (store *GardenStore) GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error) {
	container, err := store.lookup(logger, guid)
	if err != nil {
		return nil, err
	}

	return container.StreamOut(sourcePath)
}

func (store *GardenStore) Ping() error {
	return store.gardenClient.Ping()
}

func (store *GardenStore) Run(logger lager.Logger, container executor.Container) error {
	logger = logger.Session("run", lager.Data{
		"guid": container.Guid,
	})
	logger.Info("started")
	defer logger.Info("finished")

	gardenContainer, err := store.lookup(logger, container.Guid)
	if err != nil {
		return err
	}
	logger.Debug("found-garden-container")

	if container.State != executor.StateCreated {
		logger.Debug("container-invalid-state-transition", lager.Data{
			"current_state":  container.State,
			"expected_state": executor.StateCreated,
		})

		transitionErr := executor.ErrInvalidTransition
		result := executor.ContainerRunResult{
			Failed:        true,
			FailureReason: transitionErr.Error(),
		}

		err := store.transitionToComplete(logger, gardenContainer, result)
		if err != nil {
			logger.Error("failed-transition-to-complete", err)
		}

		return transitionErr
	}

	logStreamer := log_streamer.New(
		container.LogConfig.Guid,
		container.LogConfig.SourceName,
		container.LogConfig.Index,
	)

	var setupStep, actionStep, monitorStep steps.Step

	if container.Setup != nil {
		setupStep = store.transformer.StepFor(
			logStreamer,
			container.Setup,
			gardenContainer,
			container.ExternalIP,
			container.Ports,
			logger.Session("setup"),
		)
	}

	actionStep = store.transformer.StepFor(
		logStreamer,
		container.Action,
		gardenContainer,
		container.ExternalIP,
		container.Ports,
		logger.Session("action"),
	)

	hasStartedRunning := make(chan struct{}, 1)

	if container.Monitor != nil {
		monitorStep = steps.NewMonitor(
			func() steps.Step {
				return store.transformer.StepFor(
					logStreamer,
					container.Monitor,
					gardenContainer,
					container.ExternalIP,
					container.Ports,
					logger.Session("monitor-run"),
				)
			},
			hasStartedRunning,
			logger.Session("monitor"),
			store.clock,
			logStreamer,
			time.Duration(container.StartTimeout)*time.Second,
			store.healthyMonitoringInterval,
			store.unhealthyMonitoringInterval,
		)
	}

	var longLivedAction steps.Step
	if monitorStep != nil {
		longLivedAction = steps.NewCodependent([]steps.Step{actionStep, monitorStep})
	} else {
		longLivedAction = actionStep

		// this container isn't monitored, so we mark it running right away
		hasStartedRunning <- struct{}{}
	}

	var step steps.Step
	if setupStep != nil {
		step = steps.NewSerial([]steps.Step{setupStep, longLivedAction})
	} else {
		step = longLivedAction
	}

	store.runStepProcess(logger, step, hasStartedRunning, gardenContainer, container.Guid)

	return nil
}

func (store *GardenStore) runStepProcess(
	logger lager.Logger,
	step steps.Step,
	hasStartedRunning <-chan struct{},
	gardenContainer garden.Container,
	guid string,
) {
	process := ifrit.Invoke(ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		logger := logger.Session("run-step-process")
		logger.Info("started")
		defer logger.Info("finished")
		seqComplete := make(chan error)

		close(ready)

		go func() {
			seqComplete <- step.Perform()
		}()

		result := executor.ContainerRunResult{}

		toldToStop := false

	OUTER_LOOP:
		for {
			select {
			case <-signals:
				signals = nil
				toldToStop = true

				logger.Info("signaled")
				step.Cancel()

			case <-hasStartedRunning:
				hasStartedRunning = nil
				logger.Info("transitioning-to-running")
				err := store.transitionToRunning(logger, gardenContainer)
				if err != nil {
					logger.Error("failed-transitioning-to-running", err)
					result.Failed = true
					result.FailureReason = err.Error()
					break OUTER_LOOP
				}
				logger.Info("succeeded-transitioning-to-running")

			case err := <-seqComplete:
				if err == nil {
					logger.Info("step-finished-normally")
				} else if toldToStop {
					logger.Info("step-cancelled")
					result.Stopped = true
				} else {
					logger.Info("step-finished-with-error", lager.Data{"error": err.Error()})
					result.Failed = true
					result.FailureReason = err.Error()
				}

				break OUTER_LOOP
			}
		}

		logger.Info("transitioning-to-complete")
		err := store.transitionToComplete(logger, gardenContainer, result)
		if err != nil {
			logger.Error("failed-transitioning-to-complete", err)
			return nil
		}
		logger.Info("succeeded-transitioning-to-complete")

		return nil
	}))

	store.processesL.Lock()
	store.runningProcesses[guid] = process
	numProcesses := len(store.runningProcesses)
	store.processesL.Unlock()

	logger.Info("stored-step-process", lager.Data{"num-step-processes": numProcesses})
}

func (store *GardenStore) freeStepProcess(logger lager.Logger, guid string) (ifrit.Process, bool) {
	logger = logger.Session("freeing-step-process")
	logger.Info("started")
	defer logger.Info("finished")

	store.processesL.Lock()
	process, found := store.runningProcesses[guid]
	if found {
		delete(store.runningProcesses, guid)
	}
	store.processesL.Unlock()

	if !found {
		return nil, false
	}

	logger.Info("interrupting-process")
	process.Signal(os.Interrupt)
	return process, true
}

func (store *GardenStore) transitionToRunning(logger lager.Logger, gardenContainer garden.Container) error {
	err := gardenContainer.SetProperty(ContainerStateProperty, string(executor.StateRunning))
	if err != nil {
		return err
	}

	executorContainer, err := store.exchanger.Info(logger, gardenContainer)
	if err != nil {
		return err
	}

	store.eventEmitter.Emit(executor.NewContainerRunningEvent(executorContainer))

	return nil
}

func (store *GardenStore) transitionToComplete(logger lager.Logger, gardenContainer garden.Container, result executor.ContainerRunResult) error {
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

	executorContainer, err := store.exchanger.Info(logger, gardenContainer)
	if err != nil {
		return err
	}

	store.eventEmitter.Emit(executor.NewContainerCompleteEvent(executorContainer))

	return nil
}
