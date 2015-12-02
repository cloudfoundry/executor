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
	RemainingResources(logger lager.Logger) executor.ExecutorResources

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

	containers nodeMap

	runningProcesses     map[string]runningProcess
	runningProcessesLock sync.Mutex

	eventEmitter event.Hub

	clock clock.Clock
}

type runningProcess struct {
	action steps.Step
	done   chan struct{}
}

func New(
	ownerName string,
	iNodeLimit uint64,
	maxCPUShares uint64,
	totalCapacity *executor.ExecutorResources,
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
		containers:       newNodeMap(totalCapacity),
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
	err := cs.containers.Add(newStoreNode(container))
	if err != nil {
		logger.Error("failed-to-reserve", err)
		return executor.Container{}, err
	}

	cs.eventEmitter.Emit(executor.NewContainerReservedEvent(container))
	return container, nil
}

func (cs *containerStore) Initialize(logger lager.Logger, req *executor.RunRequest) error {
	logger = logger.Session("containerstore-initialize", lager.Data{"guid": req.Guid})
	logger.Debug("starting")
	defer logger.Debug("complete")

	node, err := cs.containers.Get(req.Guid)
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

	node, err = cs.containers.CAS(node)
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

	node, err := cs.containers.Get(guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return executor.Container{}, err
	}

	if node.State != executor.StateInitializing {
		logger.Error("failed-to-create", executor.ErrInvalidTransition)
		return executor.Container{}, executor.ErrInvalidTransition
	}

	node.State = executor.StateCreated

	logStreamer := logStreamerFromContainer(node.Container)
	fmt.Fprintf(logStreamer.Stdout(), "Creating container\n")
	node, err = cs.createInGarden(logger, node)
	if err != nil {
		logger.Error("failed-to-create-container", err)
		fmt.Fprintf(logStreamer.Stderr(), "Failed to create container\n")
		return executor.Container{}, err
	}
	fmt.Fprintf(logStreamer.Stdout(), "Successfully created container\n")

	node, err = cs.containers.CAS(node)
	if err != nil {
		logger.Error("failed-to-cas", err)
		destroyErr := cs.gardenClient.Destroy(node.Guid)
		if destroyErr != nil {
			logger.Error("failed-to-destroy", destroyErr)
		}
		return executor.Container{}, err
	}

	return node.Container, nil
}

func (cs *containerStore) createInGarden(logger lager.Logger, node storeNode) (storeNode, error) {
	diskScope := garden.DiskLimitScopeExclusive
	if node.DiskScope == executor.TotalDiskLimit {
		diskScope = garden.DiskLimitScopeTotal
	}

	containerSpec := garden.ContainerSpec{
		Handle:     node.Guid,
		Privileged: node.Privileged,
		RootFSPath: node.RootFSPath,
		Limits: garden.Limits{
			Memory: garden.MemoryLimits{
				LimitInBytes: uint64(node.MemoryMB * 1024 * 1024),
			},
			Disk: garden.DiskLimits{
				ByteHard:  uint64(node.DiskMB * 1024 * 1024),
				InodeHard: cs.iNodeLimit,
				Scope:     diskScope,
			},
			CPU: garden.CPULimits{
				LimitInShares: uint64(float64(cs.maxCPUShares) * float64(node.CPUWeight) / 100.0),
			},
		},
		Properties: garden.Properties{
			ContainerOwnerProperty: cs.ownerName,
		},
	}

	for _, envVar := range node.Env {
		containerSpec.Env = append(containerSpec.Env, envVar.Name+"="+envVar.Value)
	}

	netOutRules := []garden.NetOutRule{}
	for _, rule := range node.EgressRules {
		if err := rule.Validate(); err != nil {
			logger.Error("invalid-egress-rule", err)
			return storeNode{}, err
		}

		netOutRule, err := securityGroupRuleToNetOutRule(rule)
		if err != nil {
			logger.Error("failed-to-convert-to-net-out-rule", err)
			return storeNode{}, err
		}

		netOutRules = append(netOutRules, netOutRule)
	}

	logger.Debug("creating-container-in-garden")
	gardenContainer, err := cs.gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-to-creating-container-in-garden", err)
		return storeNode{}, err
	}
	logger.Debug("created-container-in-garden")

	for _, rule := range netOutRules {
		logger.Debug("net-out")
		err = gardenContainer.NetOut(rule)
		if err != nil {
			destroyErr := cs.gardenClient.Destroy(node.Guid)
			if destroyErr != nil {
				logger.Error("failed-destroy-container", err)
			}
			logger.Error("net-out-failed", err)
			return storeNode{}, err
		}
		logger.Debug("net-out-complete")
	}

	if node.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(node.Ports))
		for i, portMapping := range node.Ports {
			logger.Debug("net-in")
			actualHost, actualContainerPort, err := gardenContainer.NetIn(uint32(portMapping.HostPort), uint32(portMapping.ContainerPort))
			if err != nil {
				logger.Error("net-in-failed", err)

				destroyErr := cs.gardenClient.Destroy(node.Guid)
				if destroyErr != nil {
					logger.Error("failed-destroy-container", destroyErr)
				}

				return storeNode{}, err
			}
			logger.Debug("net-in-complete")
			actualPortMappings[i].ContainerPort = uint16(actualContainerPort)
			actualPortMappings[i].HostPort = uint16(actualHost)
		}

		node.Ports = actualPortMappings
	}

	logger.Debug("container-info")
	info, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-container-info", err)

		destroyErr := cs.gardenClient.Destroy(node.Guid)
		if destroyErr != nil {
			logger.Error("failed-destroy-container", destroyErr)
		}

		return storeNode{}, err
	}
	logger.Debug("container-info-complete")

	node.ExternalIP = info.ExternalIP
	node.GardenContainer = gardenContainer

	return node, nil
}

func (cs *containerStore) Run(logger lager.Logger, guid string) error {
	logger = logger.Session("containerstore-run")

	logger.Info("starting")
	defer logger.Info("complete")

	logger.Debug("getting-container")
	node, err := cs.containers.Get(guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	if node.State != executor.StateCreated {
		logger.Error("failed-to-run", err)
		return executor.ErrInvalidTransition
	}

	logStreamer := logStreamerFromContainer(node.Container)

	action, healthCheckPassed, err := cs.transformer.StepsForContainer(logger, node.Container, node.GardenContainer, logStreamer)
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

			node, err := cs.containers.Get(guid)
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
			node, err := cs.containers.Get(guid)
			if err != nil {
				logger.Error("failed-to-fetch-container", err)
				return
			}

			node.State = executor.StateRunning
			_, err = cs.containers.CAS(node)
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
	defer logger.Info("complete")

	node, err := cs.containers.Get(guid)
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
	node, err := cs.containers.CAS(node)
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

	node, err := cs.containers.Get(guid)
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
	node, err := cs.containers.CAS(node)
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

	node, err := cs.containers.Get(guid)
	if err != nil {
		return err
	}

	err = cs.stop(logger, node)
	if err != nil {
		logger.Error("failed-to-stop", err)
	}

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

	cs.containers.Remove(guid)

	return nil
}

func (cs *containerStore) Get(logger lager.Logger, guid string) (executor.Container, error) {
	node, err := cs.containers.Get(guid)
	return node.Container, err
}

func (cs *containerStore) List(logger lager.Logger) []executor.Container {
	logger = logger.Session("containerstore-list")

	logger.Info("starting")
	defer logger.Info("complete")

	nodes := cs.containers.List()

	containers := make([]executor.Container, 0, len(nodes))
	for i := range nodes {
		containers = append(containers, nodes[i].Container)
	}

	return containers
}

func (cs *containerStore) Metrics(logger lager.Logger) (map[string]executor.ContainerMetrics, error) {
	logger = logger.Session("containerstore-metrics")

	logger.Info("starting")
	defer logger.Info("complete")

	nodes := cs.containers.List()
	containerGuids := make([]string, 0, len(nodes))
	for i := range nodes {
		containerGuids = append(containerGuids, nodes[i].Guid)
	}

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

func (cs *containerStore) RemainingResources(logger lager.Logger) executor.ExecutorResources {
	return cs.containers.RemainingResources()
}

func (cs *containerStore) GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error) {
	logger = logger.Session("containerstore-getfiles")

	logger.Info("starting")
	defer logger.Info("complete")

	node, err := cs.containers.Get(guid)
	if err != nil {
		return nil, err
	}

	if node.GardenContainer == nil {
		// TODO THIS ERROR IS UNPROFFESSIONAL
		return nil, executor.ErrContainerNotFound
	}

	return node.GardenContainer.StreamOut(garden.StreamOutSpec{Path: sourcePath, User: "root"})
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

				now := cs.clock.Now()
				nodes := cs.containers.List()
				for i := range nodes {
					node := &nodes[i]
					if node.State != executor.StateReserved {
						continue
					}

					lifespan := now.Sub(time.Unix(0, node.AllocatedAt))
					if lifespan >= expirationTime {
						err := cs.containers.CAD(*node)
						if err != nil {
							logger.Error("failed-to-cad", err)
						}
					}
				}
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
				err := cs.reapExtraGardenContainers(logger)
				if err != nil {
					logger.Error("failed-to-reap-extra-containers", err)
				}

				err = cs.reapMissingGardenContainers(logger)
				if err != nil {
					logger.Error("failed-to-reap-missing-containers", err)
				}

			case <-signals:
				return nil
			}

			timer.Reset(reapInterval)
		}

		return nil
	})
}

func (cs *containerStore) reapExtraGardenContainers(logger lager.Logger) error {
	handles, err := cs.fetchGardenContainerHandles(logger)
	if err != nil {
		return err
	}

	for key := range handles {
		if !cs.containers.Contains(key) {
			err := cs.gardenClient.Destroy(key)
			if err != nil {
				logger.Error("failed-to-destroy-container", err)
			}
		}
	}

	return nil
}

func (cs *containerStore) reapMissingGardenContainers(logger lager.Logger) error {
	nodes := cs.containers.List()

	handles, err := cs.fetchGardenContainerHandles(logger)
	if err != nil {
		return err
	}

	for i := range nodes {
		node := &nodes[i]
		_, ok := handles[node.Guid]
		if !ok && node.IsCreated() {
			cs.complete(logger, *node, true, "container missing")
		}
	}

	return nil
}

func (cs *containerStore) fetchGardenContainerHandles(logger lager.Logger) (map[string]struct{}, error) {
	properties := garden.Properties{
		ContainerOwnerProperty: cs.ownerName,
	}

	gardenContainers, err := cs.gardenClient.Containers(properties)
	if err != nil {
		logger.Error("failed-to-fetch-containers", err)
		return nil, err
	}

	handles := make(map[string]struct{})
	for _, gardenContainer := range gardenContainers {
		handles[gardenContainer.Handle()] = struct{}{}
	}
	return handles, nil
}
