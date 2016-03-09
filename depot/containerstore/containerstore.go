package containerstore

import (
	"errors"
	"io"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/volman"
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
	// Setters
	Reserve(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error)
	Destroy(logger lager.Logger, guid string) error

	// Container Operations
	Initialize(logger lager.Logger, req *executor.RunRequest) error
	Create(logger lager.Logger, guid string) (executor.Container, error)
	Run(logger lager.Logger, guid string) error
	Stop(logger lager.Logger, guid string) error

	// Getters
	Get(logger lager.Logger, guid string) (executor.Container, error)
	List(logger lager.Logger) []executor.Container
	Metrics(logger lager.Logger) (map[string]executor.ContainerMetrics, error)
	RemainingResources(logger lager.Logger) executor.ExecutorResources
	GetFiles(logger lager.Logger, guid, sourcePath string) (io.ReadCloser, error)

	// Cleanup
	NewRegistryPruner(logger lager.Logger) ifrit.Runner
	NewContainerReaper(logger lager.Logger) ifrit.Runner
}

type ContainerConfig struct {
	OwnerName    string
	INodeLimit   uint64
	MaxCPUShares uint64

	ReservedExpirationTime time.Duration
	ReapInterval           time.Duration
}

type containerStore struct {
	containerConfig   ContainerConfig
	gardenClient      garden.Client
	dependencyManager DependencyManager
	volumeManager     volman.Manager
	transformer       transformer.Transformer
	containers        *nodeMap
	eventEmitter      event.Hub
	clock             clock.Clock

	trustedSystemCertificatesPath string
}

func New(
	containerConfig ContainerConfig,
	totalCapacity *executor.ExecutorResources,
	gardenClient garden.Client,
	dependencyManager DependencyManager,
	volumeManager volman.Manager,
	clock clock.Clock,
	eventEmitter event.Hub,
	transformer transformer.Transformer,
	trustedSystemCertificatesPath string,
) ContainerStore {
	return &containerStore{
		containerConfig:   containerConfig,
		gardenClient:      gardenClient,
		dependencyManager: dependencyManager,
		volumeManager:     volumeManager,
		containers:        newNodeMap(totalCapacity),
		eventEmitter:      eventEmitter,
		transformer:       transformer,
		clock:             clock,
		trustedSystemCertificatesPath: trustedSystemCertificatesPath,
	}
}

func (cs *containerStore) Reserve(logger lager.Logger, req *executor.AllocationRequest) (executor.Container, error) {
	logger = logger.Session("containerstore-reserve", lager.Data{"guid": req.Guid})
	logger.Debug("starting")
	defer logger.Debug("complete")

	container := executor.NewReservedContainerFromAllocationRequest(req, cs.clock.Now().UnixNano())

	err := cs.containers.Add(
		newStoreNode(&cs.containerConfig,
			container,
			cs.gardenClient,
			cs.dependencyManager,
			cs.volumeManager,
			cs.eventEmitter,
			cs.transformer,
			cs.trustedSystemCertificatesPath,
		))

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

	err = node.Initialize(logger, req)
	if err != nil {
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

	err = node.Create(logger)
	if err != nil {
		return executor.Container{}, err
	}

	return node.Info(), nil
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

	err = node.Run(logger)
	if err != nil {
		logger.Error("failed-to-run-container", err)
		return err
	}

	return nil
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

	err = node.Stop(logger)
	if err != nil {
		logger.Error("failed-to-stop-container", err)
		return err
	}

	return nil
}

func (cs *containerStore) Destroy(logger lager.Logger, guid string) error {
	logger = logger.Session("containerstore.destroy", lager.Data{"Guid": guid})

	logger.Info("starting")
	defer logger.Info("complete")

	node, err := cs.containers.Get(guid)
	if err != nil {
		logger.Error("failed-to-get-container", err)
		return err
	}

	err = node.Destroy(logger)
	if err != nil {
		logger.Error("failed-to-destroy-container", err)
	}

	cs.containers.Remove(guid)

	return err
}

func (cs *containerStore) Get(logger lager.Logger, guid string) (executor.Container, error) {
	node, err := cs.containers.Get(guid)
	if err != nil {
		return executor.Container{}, err
	}

	return node.Info(), nil
}

func (cs *containerStore) List(logger lager.Logger) []executor.Container {
	logger = logger.Session("containerstore-list")

	logger.Info("starting")
	defer logger.Info("complete")

	nodes := cs.containers.List()

	containers := make([]executor.Container, 0, len(nodes))
	for i := range nodes {
		containers = append(containers, nodes[i].Info())
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
		containerGuids = append(containerGuids, nodes[i].Info().Guid)
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

	return node.GetFiles(logger, sourcePath)
}

func (cs *containerStore) NewRegistryPruner(logger lager.Logger) ifrit.Runner {
	return newRegistryPruner(logger, &cs.containerConfig, cs.clock, cs.containers)
}

func (cs *containerStore) NewContainerReaper(logger lager.Logger) ifrit.Runner {
	return newContainerReaper(logger, &cs.containerConfig, cs.clock, cs.containers, cs.gardenClient)
}
