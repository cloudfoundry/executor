package containerstore

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/steps"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/garden/server"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
	"github.com/pivotal-golang/lager"
)

const ContainerInitializationFailedMessage = "failed to initialize container"
const ContainerExpirationMessage = "expired container"
const ContainerMissingMessage = "missing garden container"

const GardenContainerCreationDuration = metric.Duration("GardenContainerCreationDuration")

type storeNode struct {
	modifiedIndex uint

	info     executor.Container
	infoLock *sync.Mutex

	opLock          *sync.Mutex
	gardenClient    garden.Client
	gardenContainer garden.Container
	eventEmitter    event.Hub
	transformer     transformer.Transformer
	process         *runningProcess
	config          *ContainerConfig
}

type runningProcess struct {
	action            steps.Step
	done              chan struct{}
	healthCheckPassed <-chan struct{}
}

func newRunningProcess(action steps.Step, healthCheckPassed <-chan struct{}) *runningProcess {
	return &runningProcess{action: action, done: make(chan struct{}), healthCheckPassed: healthCheckPassed}
}

func newStoreNode(
	config *ContainerConfig,
	container executor.Container,
	gardenClient garden.Client,
	eventEmitter event.Hub,
	transformer transformer.Transformer,
) *storeNode {
	return &storeNode{
		config:        config,
		info:          container,
		infoLock:      &sync.Mutex{},
		opLock:        &sync.Mutex{},
		gardenClient:  gardenClient,
		eventEmitter:  eventEmitter,
		transformer:   transformer,
		modifiedIndex: 0,
	}
}

func (n *storeNode) acquireOpLock(logger lager.Logger) {
	startTime := time.Now()
	n.opLock.Lock()
	logger.Info("ops-lock-aquired", lager.Data{"lock-wait-time": time.Now().Sub(startTime)})
}

func (n *storeNode) releaseOpLock(logger lager.Logger) {
	n.opLock.Unlock()
	logger.Info("ops-lock-released")
}

func (n *storeNode) Info() executor.Container {
	n.infoLock.Lock()
	defer n.infoLock.Unlock()

	return n.info.Copy()
}

func (n *storeNode) GetFiles(logger lager.Logger, sourcePath string) (io.ReadCloser, error) {
	if n.gardenContainer == nil {
		return nil, executor.ErrContainerNotFound
	}

	return n.gardenContainer.StreamOut(garden.StreamOutSpec{Path: sourcePath, User: "root"})
}

func (n *storeNode) Initialize(logger lager.Logger, req *executor.RunRequest) error {
	n.infoLock.Lock()
	defer n.infoLock.Unlock()

	err := n.info.TransistionToInitialize(req)

	if err != nil {
		logger.Error("failed-to-initialize", err)
		return err
	}
	return nil
}

func (n *storeNode) Create(logger lager.Logger) error {
	n.acquireOpLock(logger)
	defer n.releaseOpLock(logger)

	var initialized bool
	n.infoLock.Lock()
	initialized = n.info.State == executor.StateInitializing
	n.infoLock.Unlock()
	if !initialized {
		logger.Error("failed-to-create", executor.ErrInvalidTransition)
		return executor.ErrInvalidTransition
	}

	logStreamer := logStreamerFromContainer(n.info)
	fmt.Fprintf(logStreamer.Stdout(), "Creating container\n")
	err := n.createInGarden(logger)
	if err != nil {
		logger.Error("failed-to-create-container", err)
		fmt.Fprintf(logStreamer.Stderr(), "Failed to create container\n")
		n.complete(logger, true, ContainerInitializationFailedMessage)
		return err
	}
	fmt.Fprintf(logStreamer.Stdout(), "Successfully created container\n")

	n.infoLock.Lock()
	err = n.info.TransistionToCreate()
	n.infoLock.Unlock()
	if err != nil {
		logger.Error("failed-to-transition-to-created", err)
		n.complete(logger, true, ContainerInitializationFailedMessage)
		return err
	}

	return nil
}

func (n *storeNode) createInGarden(logger lager.Logger) error {
	info := n.info.Copy()

	diskScope := garden.DiskLimitScopeExclusive
	if info.DiskScope == executor.TotalDiskLimit {
		diskScope = garden.DiskLimitScopeTotal
	}

	containerSpec := garden.ContainerSpec{
		Handle:     info.Guid,
		Privileged: info.Privileged,
		RootFSPath: info.RootFSPath,
		Limits: garden.Limits{
			Memory: garden.MemoryLimits{
				LimitInBytes: uint64(info.MemoryMB * 1024 * 1024),
			},
			Disk: garden.DiskLimits{
				ByteHard:  uint64(info.DiskMB * 1024 * 1024),
				InodeHard: n.config.INodeLimit,
				Scope:     diskScope,
			},
			CPU: garden.CPULimits{
				LimitInShares: uint64(float64(n.config.MaxCPUShares) * float64(info.CPUWeight) / 100.0),
			},
		},
		Properties: garden.Properties{
			ContainerOwnerProperty: n.config.OwnerName,
		},
	}

	for _, envVar := range info.Env {
		containerSpec.Env = append(containerSpec.Env, envVar.Name+"="+envVar.Value)
	}

	netOutRules := []garden.NetOutRule{}
	for _, rule := range info.EgressRules {
		if err := rule.Validate(); err != nil {
			logger.Error("invalid-egress-rule", err)
			return err
		}

		netOutRule, err := securityGroupRuleToNetOutRule(rule)
		if err != nil {
			logger.Error("failed-to-convert-to-net-out-rule", err)
			return err
		}

		netOutRules = append(netOutRules, netOutRule)
	}

	logger.Info("creating-container-in-garden")
	startTime := time.Now()
	gardenContainer, err := n.gardenClient.Create(containerSpec)
	if err != nil {
		logger.Error("failed-to-creating-container-in-garden", err)
		return err
	}
	GardenContainerCreationDuration.Send(time.Now().Sub(startTime))
	logger.Info("created-container-in-garden")

	for _, rule := range netOutRules {
		logger.Debug("net-out")
		err = gardenContainer.NetOut(rule)
		if err != nil {
			destroyErr := n.gardenClient.Destroy(n.info.Guid)
			if destroyErr != nil {
				logger.Error("failed-destroy-container", err)
			}
			logger.Error("net-out-failed", err)
			return err
		}
		logger.Debug("net-out-complete")
	}

	if info.Ports != nil {
		actualPortMappings := make([]executor.PortMapping, len(info.Ports))
		for i, portMapping := range info.Ports {
			logger.Debug("net-in")
			actualHost, actualContainerPort, err := gardenContainer.NetIn(uint32(portMapping.HostPort), uint32(portMapping.ContainerPort))
			if err != nil {
				logger.Error("net-in-failed", err)

				destroyErr := n.gardenClient.Destroy(info.Guid)
				if destroyErr != nil {
					logger.Error("failed-destroy-container", destroyErr)
				}

				return err
			}
			logger.Debug("net-in-complete")
			actualPortMappings[i].ContainerPort = uint16(actualContainerPort)
			actualPortMappings[i].HostPort = uint16(actualHost)
		}

		info.Ports = actualPortMappings
	}

	logger.Debug("container-info")
	gardenInfo, err := gardenContainer.Info()
	if err != nil {
		logger.Error("failed-container-info", err)

		destroyErr := n.gardenClient.Destroy(info.Guid)
		if destroyErr != nil {
			logger.Error("failed-destroy-container", destroyErr)
		}

		return err
	}
	logger.Debug("container-info-complete")

	info.ExternalIP = gardenInfo.ExternalIP
	n.gardenContainer = gardenContainer

	n.infoLock.Lock()
	n.info = info
	n.infoLock.Unlock()

	return nil
}

func (n *storeNode) Run(logger lager.Logger) error {
	n.acquireOpLock(logger)
	defer n.releaseOpLock(logger)

	if n.info.State != executor.StateCreated {
		logger.Error("failed-to-run", executor.ErrInvalidTransition)
		return executor.ErrInvalidTransition
	}

	logStreamer := logStreamerFromContainer(n.info)

	action, healthCheckPassed, err := n.transformer.StepsForContainer(logger, n.info, n.gardenContainer, logStreamer)
	if err != nil {
		logger.Error("failed-to-build-steps", err)
		return err
	}

	process := newRunningProcess(action, healthCheckPassed)
	n.process = process
	go n.run(logger)
	return nil
}

func (n *storeNode) run(logger lager.Logger) {
	resultCh := make(chan error)
	go func() {
		resultCh <- n.process.action.Perform()
	}()

	for {
		select {
		case err := <-resultCh:
			defer close(n.process.done)
			var failed bool
			var failureReason string

			if err != nil {
				failed = true
				failureReason = err.Error()
			}

			n.complete(logger, failed, failureReason)
			return

		case <-n.process.healthCheckPassed:
			n.infoLock.Lock()
			n.info.State = executor.StateRunning
			info := n.info
			n.infoLock.Unlock()
			go n.eventEmitter.Emit(executor.NewContainerRunningEvent(info))
		}
	}
}

func (n *storeNode) Stop(logger lager.Logger) error {
	n.acquireOpLock(logger)
	defer n.releaseOpLock(logger)

	return n.stop(logger)
}

func (n *storeNode) stop(logger lager.Logger) error {
	n.infoLock.Lock()
	n.info.RunResult.Stopped = true
	n.infoLock.Unlock()

	if n.process != nil {
		n.process.action.Cancel()
		<-n.process.done
	} else {
		n.complete(logger, true, "stopped-before-running")
	}
	return nil
}

func (n *storeNode) Destroy(logger lager.Logger) error {
	n.acquireOpLock(logger)
	defer n.releaseOpLock(logger)

	err := n.stop(logger)
	if err != nil {
		return err
	}

	logger.Debug("destroying-garden-container")
	err = n.gardenClient.Destroy(n.info.Guid)
	if err != nil {
		if _, ok := err.(garden.ContainerNotFoundError); ok {
			logger.Error("container-not-found-in-garden", err)
		} else if err.Error() == server.ErrConcurrentDestroy.Error() {
			logger.Error("container-destroy-in-progress", err)
		} else {
			logger.Error("failed-to-delete-garden-container", err)
			return err
		}
	}

	logger.Debug("destroyed-garden-container")
	return nil
}

func (n *storeNode) Expire(logger lager.Logger, now time.Time) bool {
	n.infoLock.Lock()
	defer n.infoLock.Unlock()

	if n.info.State != executor.StateReserved {
		return false
	}

	lifespan := now.Sub(time.Unix(0, n.info.AllocatedAt))
	if lifespan >= n.config.ReservedExpirationTime {
		n.info.TransitionToComplete(true, ContainerExpirationMessage)
		go n.eventEmitter.Emit(executor.NewContainerCompleteEvent(n.info))
		return true
	}

	return false
}

func (n *storeNode) Reap(logger lager.Logger) bool {
	n.infoLock.Lock()
	defer n.infoLock.Unlock()

	if n.info.IsCreated() {
		n.info.TransitionToComplete(true, ContainerMissingMessage)
		go n.eventEmitter.Emit(executor.NewContainerCompleteEvent(n.info))
		return true
	}

	return false
}

func (n *storeNode) complete(logger lager.Logger, failed bool, failureReason string) {
	n.infoLock.Lock()
	defer n.infoLock.Unlock()
	n.info.TransitionToComplete(failed, failureReason)

	go n.eventEmitter.Emit(executor.NewContainerCompleteEvent(n.info))
}
