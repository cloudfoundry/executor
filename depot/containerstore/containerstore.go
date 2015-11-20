package containerstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

const (
	tagPropertyPrefix      = "tag:"
	executorPropertyPrefix = "executor:"

	ContainerOwnerProperty         = executorPropertyPrefix + "owner"
	ContainerStateProperty         = executorPropertyPrefix + "state"
	ContainerAllocatedAtProperty   = executorPropertyPrefix + "allocated-at"
	ContainerRootfsProperty        = executorPropertyPrefix + "rootfs"
	ContainerLogProperty           = executorPropertyPrefix + "log-config"
	ContainerMetricsConfigProperty = executorPropertyPrefix + "metrics-config"
	ContainerResultProperty        = executorPropertyPrefix + "result"
	ContainerMemoryMBProperty      = executorPropertyPrefix + "memory-mb"
	ContainerDiskMBProperty        = executorPropertyPrefix + "disk-mb"
	ContainerCPUWeightProperty     = executorPropertyPrefix + "cpu-weight"
	ContainerStartTimeoutProperty  = executorPropertyPrefix + "start-timeout"
)

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
	container     executor.Container
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
	container := executor.NewReservedContainerFromAllocationRequest(req, time.Now().Unix())

	_, err := cs.get(logger, container.Guid)
	if err == nil {
		return executor.Container{}, executor.ErrContainerGuidNotAvailable
	}

	cs.containersLock.Lock()
	defer cs.containersLock.Unlock()
	cs.containers[container.Guid] = storeNode{container: container, modifiedIndex: 0}

	go cs.eventEmitter.Emit(executor.NewContainerReservedEvent(container))

	return container, nil
}

func (cs *containerStore) Initialize(logger lager.Logger, req *executor.RunRequest) error {
	node, err := cs.get(logger, req.Guid)
	if err != nil {
		return err
	}

	if node.container.State != executor.StateReserved {
		return executor.ErrInvalidTransition
	}

	node.container.State = executor.StateInitializing
	node.container.RunInfo = req.RunInfo
	node.container.Tags.Add(req.Tags)

	node, err = cs.compareAndSwap(logger, node)
	if err != nil {
		return err
	}

	return nil
}

func (cs *containerStore) Create(logger lager.Logger, guid string) (executor.Container, error) {
	logger = logger.Session("container-store.create")

	logger.Debug("obtaining-container")
	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return executor.Container{}, err
	}

	container := node.container

	if container.State != executor.StateInitializing {
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

	node.container = container

	node, err = cs.compareAndSwap(logger, node)
	if err != nil {
		destroyErr := cs.gardenClient.Destroy(node.container.Guid)
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

	logProperty, err := json.Marshal(container.LogConfig)
	if err != nil {
		logger.Error("failed-to-marshal-log-config", err)
		return executor.Container{}, err
	}

	metricsProperty, err := json.Marshal(container.MetricsConfig)
	if err != nil {
		logger.Error("failed-to-marshal-metrics-config", err)
		return executor.Container{}, err
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
			ContainerOwnerProperty:         cs.ownerName,
			ContainerStateProperty:         string(container.State),
			ContainerAllocatedAtProperty:   fmt.Sprintf("%d", container.AllocatedAt),
			ContainerStartTimeoutProperty:  fmt.Sprintf("%d", container.StartTimeout),
			ContainerRootfsProperty:        container.RootFSPath,
			ContainerLogProperty:           string(logProperty),
			ContainerMetricsConfigProperty: string(metricsProperty),
			ContainerMemoryMBProperty:      fmt.Sprintf("%d", container.MemoryMB),
			ContainerDiskMBProperty:        fmt.Sprintf("%d", container.DiskMB),
			ContainerCPUWeightProperty:     fmt.Sprintf("%d", container.CPUWeight),
		},
	}

	for name, value := range container.Tags {
		containerSpec.Properties[tagPropertyPrefix+name] = value
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

	gardenContainer, err := cs.gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-to-create-container-in-garden", err)
		return executor.Container{}, err
	}

	for _, rule := range netOutRules {
		err = gardenContainer.NetOut(rule)
		if err != nil {
			destroyErr := cs.gardenClient.Destroy(container.Guid)
			if destroyErr != nil {
				logger.Error("failed-destroy-container", err)
			}
			logger.Error("failed-to-net-out", err)
			return executor.Container{}, err
		}
	}

	if container.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(container.Ports))
		for i, portMapping := range container.Ports {
			actualHost, actualContainerPort, err := gardenContainer.NetIn(uint32(portMapping.HostPort), uint32(portMapping.ContainerPort))
			if err != nil {
				logger.Error("failed-to-net-in", err)

				destroyErr := cs.gardenClient.Destroy(container.Guid)
				if destroyErr != nil {
					logger.Error("failed-destroy-container", destroyErr)
				}

				return executor.Container{}, err
			}
			actualPortMappings[i].ContainerPort = uint16(actualContainerPort)
			actualPortMappings[i].HostPort = uint16(actualHost)
		}

		container.Ports = actualPortMappings
	}

	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-container-info", err)

		destroyErr := cs.gardenClient.Destroy(container.Guid)
		if destroyErr != nil {
			logger.Error("failed-destroy-container", destroyErr)
		}

		return executor.Container{}, err
	}
	container.ExternalIP = info.ExternalIP
	container.GardenContainer = gardenContainer

	return container, nil
}

func (cs *containerStore) Run(logger lager.Logger, guid string) error {
	logger = logger.Session("container-store.run")

	logger.Debug("getting-container")
	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	container := node.container

	if container.State != executor.StateCreated {
		return executor.ErrInvalidTransition
	}

	logStreamer := logStreamerFromContainer(container)

	action, healthCheckPassed, err := cs.transformer.StepsForContainer(logger, container, logStreamer)
	if err != nil {
		return err
	}

	doneChan := make(chan struct{})
	go func() {
		resultCh := make(chan error)
		go func() {
			resultCh <- action.Perform()
		}()

		for {
			select {
			case err := <-resultCh:
				defer close(doneChan)
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
				node.container.State = executor.StateRunning
				_, err = cs.compareAndSwap(logger, node)
				if err != nil {
					logger.Error("failed-to-transition-to-running", err)
					return
				}
			}
		}
	}()

	cs.runningProcessesLock.Lock()
	defer cs.runningProcessesLock.Unlock()
	cs.runningProcesses[container.Guid] = runningProcess{action: action, done: doneChan}

	return nil
}

func (cs *containerStore) Fail(logger lager.Logger, guid, reason string) (executor.Container, error) {
	logger = logger.Session("container-store.fail")

	node, err := cs.get(logger, guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return executor.Container{}, err
	}

	if node.container.State == executor.StateCompleted {
		logger.Error("invalid-transition", executor.ErrInvalidTransition)
		return executor.Container{}, executor.ErrInvalidTransition
	}

	node, err = cs.complete(logger, node, true, reason)
	if err != nil {
		logger.Error("failed-to-complete-node", err)
		return executor.Container{}, err
	}

	return node.container, nil
}

func (cs *containerStore) complete(logger lager.Logger, node storeNode, failed bool, failureReason string) (storeNode, error) {
	node.container.RunResult.Failed = failed
	node.container.RunResult.FailureReason = failureReason

	node.container.State = executor.StateCompleted
	node, err := cs.compareAndSwap(logger, node)
	if err != nil {
		return node, err
	}

	go cs.eventEmitter.Emit(executor.NewContainerCompleteEvent(node.container))

	return node, nil
}

func (cs *containerStore) Stop(logger lager.Logger, guid string) error {
	node, err := cs.get(logger, guid)
	if err != nil {
		return err
	}

	return cs.stop(logger, node)
}

func (cs *containerStore) stop(logger lager.Logger, node storeNode) error {
	cs.runningProcessesLock.Lock()
	process, ok := cs.runningProcesses[node.container.Guid]
	if !ok {
		return executor.ErrNoProcessToStop
	}
	cs.runningProcessesLock.Unlock()

	node.container.RunResult.Stopped = true
	node, err := cs.compareAndSwap(logger, node)
	if err != nil {
		logger.Error("failed-to-cas-container", err)
		return err
	}

	process.action.Cancel()
	<-process.done

	cs.runningProcessesLock.Lock()
	delete(cs.runningProcesses, node.container.Guid)
	cs.runningProcessesLock.Unlock()

	return nil
}

func (cs *containerStore) Destroy(logger lager.Logger, guid string) error {
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

	err = cs.gardenClient.Destroy(guid)
	if err != nil {
		if _, ok := err.(garden.ContainerNotFoundError); ok {
			logger.Error("container-not-found-in-garden", err)
			return nil
		}
		logger.Error("failed-to-create-garden-container", err)
		return err
	}

	return nil
}

func (cs *containerStore) Get(logger lager.Logger, guid string) (executor.Container, error) {
	node, err := cs.get(logger, guid)
	return node.container, err
}

func (cs *containerStore) get(logger lager.Logger, guid string) (storeNode, error) {
	cs.containersLock.RLock()
	defer cs.containersLock.RUnlock()

	node, ok := cs.containers[guid]
	if !ok {
		return storeNode{}, executor.ErrContainerNotFound
	}
	return node, nil
}

func (cs *containerStore) List(logger lager.Logger) []executor.Container {
	cs.containersLock.RLock()
	defer cs.containersLock.RUnlock()

	containers := make([]executor.Container, 0, len(cs.containers))

	for key := range cs.containers {
		containers = append(containers, cs.containers[key].container)
	}

	return containers
}

func (cs *containerStore) Metrics(logger lager.Logger) (map[string]executor.ContainerMetrics, error) {
	cs.containersLock.RLock()
	containerGuids := make([]string, 0, len(cs.containers))
	for key := range cs.containers {
		containerGuids = append(containerGuids, key)
	}
	cs.containersLock.RUnlock()

	gardenMetrics, err := cs.gardenClient.BulkMetrics(containerGuids)
	if err != nil {
		return nil, err
	}

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
	container, err := cs.Get(logger, guid)
	if err != nil {
		return nil, err
	}

	if container.GardenContainer == nil {
		return nil, executor.ErrContainerNotFound
	}

	return container.GardenContainer.StreamOut(garden.StreamOutSpec{Path: sourcePath, User: "root"})
}

func (cs *containerStore) compareAndSwap(logger lager.Logger, node storeNode) (storeNode, error) {
	cs.containersLock.Lock()
	defer cs.containersLock.Unlock()
	existingNode, ok := cs.containers[node.container.Guid]
	if !ok {
		return storeNode{}, executor.ErrContainerNotFound
	}

	if existingNode.modifiedIndex != node.modifiedIndex {
		return storeNode{}, ErrFailedToCAS
	}

	node.modifiedIndex++
	cs.containers[node.container.Guid] = node

	return node, nil
}

func logStreamerFromContainer(container executor.Container) log_streamer.LogStreamer {
	return log_streamer.New(
		container.LogConfig.Guid,
		container.LogConfig.SourceName,
		container.LogConfig.Index,
	)
}
