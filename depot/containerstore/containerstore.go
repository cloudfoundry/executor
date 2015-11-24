package containerstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

const ContainerOwnerProperty = "executor:owner"

var (
	ErrFailedToCAS = errors.New("failed-to-cas")
)

//go:generate counterfeiter -o containerstorefakes/fake_containerstore.go . ContainerStore

type ContainerStore interface {
	// Lifecycle
	Reserve(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error)
	Initialize(logger lager.Logger, req *executor.RunRequest) error
	Create(logger lager.Logger, guid string) (executor.Container, error)
	Run(logger lager.Logger, guid string) error
	Fail(logger lager.Logger, guid string, reason string) (executor.Container, error)
	Stop(logger lager.Logger, guid string) error
	Destroy(logger lager.Logger, guid string) error

	// Getters
	Get(logger lager.Logger, guid string) (executor.Container, error)
	List(logger lager.Logger) []executor.Container
	Metrics(logger lager.Logger) (map[string]executor.ContainerMetrics, error)

	// Hopefully on container?
	GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error)

	RegistryPruner(logger lager.Logger, expirationTime time.Duration) ifrit.Runner
	ContainerReaper(logger lager.Logger, reapInterval time.Duration) ifrit.Runner
}

type containerStore struct {
	ownerName    string
	iNodeLimit   uint64
	maxCPUShares uint64

	gardenClient garden.Client
	transformer  transformer.Transformer

	containers     map[string]storeNode
	containersLock sync.RWMutex

	runningProcesses     map[string]runningProcess
	runningProcessesLock sync.Mutex

	eventEmitter event.Hub

	clock clock.Clock
}

type storeNode struct {
	modifiedIndex uint
	executor.Container
}

type runningProcess struct {
	action steps.Step
	done   chan struct{}
}

func New(
	ownerName string,
	iNodeLimit uint64,
	maxCPUShares uint64,
	gardenClient garden.Client,
	clock clock.Clock,
	eventEmitter event.Hub,
	transformer transformer.Transformer,
) ContainerStore {
	return &containerStore{
		ownerName:        ownerName,
		iNodeLimit:       iNodeLimit,
		maxCPUShares:     maxCPUShares,
		gardenClient:     gardenClient,
		containers:       map[string]storeNode{},
		runningProcesses: map[string]runningProcess{},
		eventEmitter:     eventEmitter,
		transformer:      transformer,
		clock:            clock,
	}
}

func (cs *containerStore) Reserve(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error) {
	logger = logger.Session("containerstore-reserve", lager.Data{"guid": req.Guid})

	logger.Debug("starting")
	defer logger.Debug("complete")

	container := executor.NewReservedContainerFromAllocationRequest(req, cs.clock.Now().UnixNano())

	_, err := cs.get(logger, container.Guid)
	if err == nil {
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	cs.containersLock.Lock()
	cs.containers[container.Guid] = storeNode{Container: container, modifiedIndex: 0}
	cs.containersLock.Unlock()

	cs.eventEmitter.Emit(executor.NewContainerReservedEvent(container))

	return container, nil
}

func (cs *containerStore) Initialize(logger lager.Logger, req *executor.RunRequest) error {
	logger = logger.Session("containerstore-initialize", lager.Data{"guid": req.Guid})

	logger.Debug("starting")
	defer logger.Debug("complete")

	node, err := cs.get(logger, req.Guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	if node.State != executor.StateReserved {
		logger.Error("failed-to-initialize", executor.ErrInvalidTransition)
		return executor.ErrInvalidTransition
	}

	node.State = executor.StateInitializing
	node.RunInfo = req.RunInfo
	node.Tags.Add(req.Tags)

	node, err = cs.compareAndSwap(logger, node)
	if err != nil {
		logger.Error("failed-to-cas", err)
		return err
	}

	return nil
}

func (cs *containerStore) Create(logger lager.Logger, guid string) (executor.Container, error) {
	logger = logger.Session("containerstore-create", lager.Data{"guid": guid})

	logger.Info("starting")
	defer logger.Info("complete")

	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return executor.Container{}, err
	}

	container := node.Container

	if container.State != executor.StateInitializing {
		logger.Error("failed-to-create", executor.ErrInvalidTransition)
		return executor.Container{}, executor.ErrInvalidTransition
	}

	container.State = executor.StateCreated

	logStreamer := logStreamerFromContainer(container)
	fmt.Fprintf(logStreamer.Stdout(), "Creating container\n")
	container, err = cs.createInGarden(logger, container)
	if err != nil {
		logger.Error("failed-to-create-container", err)
		fmt.Fprintf(logStreamer.Stderr(), "Failed to create container\n")
		return container, err
	}
	fmt.Fprintf(logStreamer.Stdout(), "Successfully created container\n")

	node.Container = container

	node, err = cs.compareAndSwap(logger, node)
	if err != nil {
		logger.Error("failed-to-cas", err)
		destroyErr := cs.gardenClient.Destroy(node.Guid)
		if destroyErr != nil {
			logger.Error("failed-to-destroy", destroyErr)
		}
		return executor.Container{}, err
	}

	return container, nil
}

func (cs *containerStore) createInGarden(logger lager.Logger, container executor.Container) (executor.Container, error) {
	diskScope := garden.DiskLimitScopeExclusive
	if container.DiskScope == executor.TotalDiskLimit {
		diskScope = garden.DiskLimitScopeTotal
	}

	containerSpec := garden.ContainerSpec{
		Handle:     container.Guid,
		Privileged: container.Privileged,
		RootFSPath: container.RootFSPath,
		Limits: garden.Limits{
			Memory: garden.MemoryLimits{
				LimitInBytes: uint64(container.MemoryMB * 1024 * 1024),
			},
			Disk: garden.DiskLimits{
				ByteHard:  uint64(container.DiskMB * 1024 * 1024),
				InodeHard: cs.iNodeLimit,
				Scope:     diskScope,
			},
			CPU: garden.CPULimits{
				LimitInShares: uint64(float64(cs.maxCPUShares) * float64(container.CPUWeight) / 100.0),
			},
		},
		Properties: garden.Properties{
			ContainerOwnerProperty: cs.ownerName,
		},
	}

	for _, envVar := range container.Env {
		containerSpec.Env = append(containerSpec.Env, envVar.Name+"="+envVar.Value)
	}

	netOutRules := []garden.NetOutRule{}
	for _, rule := range container.EgressRules {
		if err := rule.Validate(); err != nil {
			logger.Error("invalid-egress-rule", err)
			return executor.Container{}, err
		}

		netOutRule, err := securityGroupRuleToNetOutRule(rule)
		if err != nil {
			logger.Error("failed-to-convert-to-net-out-rule", err)
			return executor.Container{}, err
		}

		netOutRules = append(netOutRules, netOutRule)
	}

	logger.Debug("creating-container-in-garden")
	gardenContainer, err := cs.gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-to-creating-container-in-garden", err)
		return executor.Container{}, err
	}
	logger.Debug("created-container-in-garden")

	for _, rule := range netOutRules {
		logger.Debug("net-out")
		err = gardenContainer.NetOut(rule)
		if err != nil {
			destroyErr := cs.gardenClient.Destroy(container.Guid)
			if destroyErr != nil {
				logger.Error("failed-destroy-container", err)
			}
			logger.Error("net-out-failed", err)
			return executor.Container{}, err
		}
		logger.Debug("net-out-complete")
	}

	if container.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(container.Ports))
		for i, portMapping := range container.Ports {
			logger.Debug("net-in")
			actualHost, actualContainerPort, err := gardenContainer.NetIn(uint32(portMapping.HostPort), uint32(portMapping.ContainerPort))
			if err != nil {
				logger.Error("net-in-failed", err)

				destroyErr := cs.gardenClient.Destroy(container.Guid)
				if destroyErr != nil {
					logger.Error("failed-destroy-container", destroyErr)
				}

				return executor.Container{}, err
			}
			logger.Debug("net-in-complete")
			actualPortMappings[i].ContainerPort = uint16(actualContainerPort)
			actualPortMappings[i].HostPort = uint16(actualHost)
		}

		container.Ports = actualPortMappings
	}

	logger.Debug("container-info")
	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-container-info", err)

		destroyErr := cs.gardenClient.Destroy(container.Guid)
		if destroyErr != nil {
			logger.Error("failed-destroy-container", destroyErr)
		}

		return executor.Container{}, err
	}
	logger.Debug("container-info-complete")

	container.ExternalIP = info.ExternalIP
	container.GardenContainer = gardenContainer

	return container, nil
}

func (cs *containerStore) Run(logger lager.Logger, guid string) error {
	logger = logger.Session("containerstore-run")

	logger.Info("starting")
	defer logger.Info("complete")

	logger.Debug("getting-container")
	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	if node.State != executor.StateCreated {
		logger.Error("failed-to-run", err)
		return executor.ErrInvalidTransition
	}

	logStreamer := logStreamerFromContainer(node.Container)

	action, healthCheckPassed, err := cs.transformer.StepsForContainer(logger, node.Container, logStreamer)
	if err != nil {
		logger.Error("failed-to-build-steps", err)
		return err
	}

	doneChan := make(chan struct{})
	process := runningProcess{action: action, done: doneChan}
	go cs.run(logger, process, healthCheckPassed, node.Guid)

	cs.runningProcessesLock.Lock()
	cs.runningProcesses[node.Guid] = process
	cs.runningProcessesLock.Unlock()

	return nil
}

func (cs *containerStore) run(logger lager.Logger, process runningProcess, healthCheckPassed <-chan struct{}, guid string) {
	resultCh := make(chan error)
	go func() {
		resultCh <- process.action.Perform()
	}()

	for {
		select {
		case err := <-resultCh:
			defer close(process.done)
			var failed bool
			var failureReason string

			if err != nil {
				failed = true
				failureReason = err.Error()
			}

			node, err := cs.get(logger, guid)
			if err != nil {
				logger.Error("failed-to-fetch-container", err)
				return
			}
			node, err = cs.complete(logger, node, failed, failureReason)
			if err != nil {
				logger.Error("failed-to-complete", err)
			}
			return

		case <-healthCheckPassed:
			node, err := cs.get(logger, guid)
			if err != nil {
				logger.Error("failed-to-fetch-container", err)
				return
			}
			node.State = executor.StateRunning
			_, err = cs.compareAndSwap(logger, node)
			if err != nil {
				logger.Error("failed-to-transition-to-running", err)
				return
			}
			cs.eventEmitter.Emit(executor.NewContainerRunningEvent(node.Container))
		}
	}
}

func (cs *containerStore) Fail(logger lager.Logger, guid, reason string) (executor.Container, error) {
	logger = logger.Session("containerstore-fail")

	logger.Info("starting")
	logger.Info("complete")

	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return executor.Container{}, err
	}

	if node.State == executor.StateCompleted {
		logger.Error("invalid-transition", executor.ErrInvalidTransition)
		return executor.Container{}, executor.ErrInvalidTransition
	}

	node, err = cs.complete(logger, node, true, reason)
	if err != nil {
		logger.Error("failed-to-complete-node", err)
		return executor.Container{}, err
	}

	return node.Container, nil
}

func (cs *containerStore) complete(logger lager.Logger, node storeNode, failed bool, failureReason string) (storeNode, error) {
	node.RunResult.Failed = failed
	node.RunResult.FailureReason = failureReason

	node.State = executor.StateCompleted
	node, err := cs.compareAndSwap(logger, node)
	if err != nil {
		logger.Error("failed-to-cas", err)
		return node, err
	}

	cs.eventEmitter.Emit(executor.NewContainerCompleteEvent(node.Container))

	return node, nil
}

func (cs *containerStore) Stop(logger lager.Logger, guid string) error {
	logger = logger.Session("containerstore-stop", lager.Data{"Guid": guid})

	logger.Info("starting")
	defer logger.Info("complete")

	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	return cs.stop(logger, node)
}

func (cs *containerStore) stop(logger lager.Logger, node storeNode) error {
	cs.runningProcessesLock.Lock()
	process, ok := cs.runningProcesses[node.Guid]
	cs.runningProcessesLock.Unlock()

	if !ok {
		return executor.ErrNoProcessToStop
	}

	node.RunResult.Stopped = true
	node, err := cs.compareAndSwap(logger, node)
	if err != nil {
		logger.Error("failed-to-cas-container", err)
		return err
	}

	process.action.Cancel()
	<-process.done

	cs.runningProcessesLock.Lock()
	delete(cs.runningProcesses, node.Guid)
	cs.runningProcessesLock.Unlock()

	return nil
}

func (cs *containerStore) Destroy(logger lager.Logger, guid string) error {
	logger = logger.Session("containerstore.destroy", lager.Data{"Guid": guid})

	logger.Info("starting")
	defer logger.Info("complete")

	node, err := cs.get(logger, guid)
	if err != nil {
		return err
	}

	err = cs.stop(logger, node)
	if err != nil {
		logger.Error("failed-to-stop", err)
	}

	cs.containersLock.Lock()
	delete(cs.containers, guid)
	cs.containersLock.Unlock()

	logger.Debug("destroying-garden-container")
	err = cs.gardenClient.Destroy(guid)
	if err != nil {
		if _, ok := err.(garden.ContainerNotFoundError); ok {
			logger.Error("container-not-found-in-garden", err)
			return nil
		}
		logger.Error("failed-to-delete-garden-container", err)
		return err
	}
	logger.Debug("destroyed-garden-container")

	return nil
}

func (cs *containerStore) Get(logger lager.Logger, guid string) (executor.Container, error) {
	node, err := cs.get(logger, guid)
	return node.Container, err
}

func (cs *containerStore) get(logger lager.Logger, guid string) (storeNode, error) {
	logger = logger.Session("containerstore-get")

	logger.Info("starting")
	defer logger.Info("complete")

	cs.containersLock.RLock()
	defer cs.containersLock.RUnlock()

	node, ok := cs.containers[guid]
	if !ok {
		logger.Error("container-not-found", executor.ErrContainerNotFound)
		return storeNode{}, executor.ErrContainerNotFound
	}
	return node, nil
}

func (cs *containerStore) List(logger lager.Logger) []executor.Container {
	logger = logger.Session("containerstore-list")

	logger.Info("starting")
	defer logger.Info("complete")

	cs.containersLock.RLock()
	defer cs.containersLock.RUnlock()

	containers := make([]executor.Container, 0, len(cs.containers))

	for key := range cs.containers {
		containers = append(containers, cs.containers[key].Container)
	}

	return containers
}

func (cs *containerStore) Metrics(logger lager.Logger) (map[string]executor.ContainerMetrics, error) {
	logger = logger.Session("containerstore-metrics")

	logger.Info("starting")
	defer logger.Info("complete")

	cs.containersLock.RLock()
	containerGuids := make([]string, 0, len(cs.containers))
	for key := range cs.containers {
		containerGuids = append(containerGuids, key)
	}
	cs.containersLock.RUnlock()

	logger.Debug("getting-metrics-in-garden")
	gardenMetrics, err := cs.gardenClient.BulkMetrics(containerGuids)
	if err != nil {
		logger.Error("getting-metrics-in-garden-failed", err)
		return nil, err
	}
	logger.Debug("getting-metrics-in-garden-complete")

	containerMetrics := map[string]executor.ContainerMetrics{}
	for _, guid := range containerGuids {
		if metricEntry, found := gardenMetrics[guid]; found {
			if metricEntry.Err == nil {
				gardenMetric := metricEntry.Metrics
				containerMetrics[guid] = executor.ContainerMetrics{
					MemoryUsageInBytes: gardenMetric.MemoryStat.TotalUsageTowardLimit,
					DiskUsageInBytes:   gardenMetric.DiskStat.ExclusiveBytesUsed,
					TimeSpentInCPU:     time.Duration(gardenMetric.CPUStat.Usage),
				}
			}
		}
	}

	return containerMetrics, nil
}

func (cs *containerStore) GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error) {
	logger = logger.Session("containerstore-getfiles")

	logger.Info("starting")
	defer logger.Info("complete")

	container, err := cs.Get(logger, guid)
	if err != nil {
		return nil, err
	}

	if container.GardenContainer == nil {
		// TODO THIS ERROR SUCKS
		return nil, executor.ErrContainerNotFound
	}

	return container.GardenContainer.StreamOut(garden.StreamOutSpec{Path: sourcePath, User: "root"})
}

func (cs *containerStore) compareAndSwap(logger lager.Logger, node storeNode) (storeNode, error) {
	logger = logger.Session("compare-and-swap")

	logger.Info("starting")
	defer logger.Info("finished")

	cs.containersLock.Lock()
	defer cs.containersLock.Unlock()

	existingNode, ok := cs.containers[node.Guid]
	if !ok {
		logger.Error("container-not-found", executor.ErrContainerNotFound)
		return storeNode{}, executor.ErrContainerNotFound
	}

	if existingNode.modifiedIndex != node.modifiedIndex {
		logger.Error("failed-to-cas", ErrFailedToCAS)
		return storeNode{}, ErrFailedToCAS
	}

	node.modifiedIndex++
	cs.containers[node.Guid] = node

	return node, nil
}

func logStreamerFromContainer(container executor.Container) log_streamer.LogStreamer {
	return log_streamer.New(
		container.LogConfig.Guid,
		container.LogConfig.SourceName,
		container.LogConfig.Index,
	)
}

func (cs *containerStore) RegistryPruner(logger lager.Logger, expirationTime time.Duration) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		ticker := cs.clock.NewTicker(expirationTime / 2)

		defer ticker.Stop()
		for {
			select {
			case <-ticker.C():
				cs.containersLock.Lock()

				for key := range cs.containers {
					node := cs.containers[key]
					if node.State != executor.StateReserved {
						continue
					}

					lifespan := cs.clock.Now().Sub(time.Unix(0, node.AllocatedAt))
					if lifespan >= expirationTime {
						delete(cs.containers, key)
					}
				}

				cs.containersLock.Unlock()
			case <-signals:
				return nil
			}
		}
		return nil
	})
}

func (cs *containerStore) ContainerReaper(logger lager.Logger, reapInterval time.Duration) ifrit.Runner {
	logger = logger.Session("container-reqper")

	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		timer := cs.clock.NewTimer(reapInterval)

		for {
			select {
			case <-timer.C():
				properties := garden.Properties{
					ContainerOwnerProperty: cs.ownerName,
				}

				gardenContainers, err := cs.gardenClient.Containers(properties)
				if err != nil {
					logger.Error("failed-to-fetch-containers", err)
					break
				}

				handles := make(map[string]struct{})
				for _, gardenContainer := range gardenContainers {
					handles[gardenContainer.Handle()] = struct{}{}
				}

				cs.containersLock.Lock()
				for key, node := range cs.containers {
					_, ok := handles[key]
					if !ok && node.IsCreated() {
						delete(cs.containers, key)
					}
				}
				cs.containersLock.Unlock()

				for key := range handles {
					_, err := cs.get(logger, key)
					if err != nil {
						err := cs.gardenClient.Destroy(key)
						if err != nil {
							logger.Error("failed-to-destroy-container", err)
						}
					}
				}

			case <-signals:
				return nil
			}

			timer.Reset(reapInterval)
		}

		return nil
	})
}
