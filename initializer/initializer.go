package initializer

import (
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/cacheddownloader"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/containermetrics"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/depot/allocationstore"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/depot/keyed_lock"
	"github.com/cloudfoundry-incubator/executor/depot/metrics"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/executor/depot/uploader"
	"github.com/cloudfoundry-incubator/executor/initializer/configuration"
	"github.com/cloudfoundry-incubator/garden"
	GardenClient "github.com/cloudfoundry-incubator/garden/client"
	GardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
)

const (
	maxConcurrentDownloads         = 5
	maxConcurrentUploads           = 5
	metricsReportInterval          = 1 * time.Minute
	containerMetricsReportInterval = 30 * time.Second
)

type executorContainers struct {
	gardenClient garden.Client
	owner        string
}

func (containers *executorContainers) Containers() ([]garden.Container, error) {
	return containers.gardenClient.Containers(garden.Properties{
		gardenstore.ContainerOwnerProperty: containers.owner,
	})
}

type Configuration struct {
	GardenNetwork string
	GardenAddr    string

	ContainerOwnerName string

	TempDir              string
	CachePath            string
	MaxCacheSizeInBytes  uint64
	AllowPrivileged      bool
	SkipCertVerify       bool
	ExportNetworkEnvVars bool

	ContainerMaxCpuShares       uint64
	ContainerInodeLimit         uint64
	HealthyMonitoringInterval   time.Duration
	UnhealthyMonitoringInterval time.Duration
	HealthCheckWorkPoolSize     int

	CreateWorkPoolSize  int
	DeleteWorkPoolSize  int
	ReadWorkPoolSize    int
	MetricsWorkPoolSize int

	RegistryPruningInterval time.Duration

	MemoryMB string
	DiskMB   string
}

const (
	defaultCreateWorkPoolSize      = 32
	defaultDeleteWorkPoolSize      = 32
	defaultReadWorkPoolSize        = 64
	defaultMetricsWorkPoolSize     = 8
	defaultHealthCheckWorkPoolSize = 64
)

var DefaultConfiguration = Configuration{
	GardenNetwork:               "unix",
	GardenAddr:                  "/tmp/garden.sock",
	MemoryMB:                    configuration.Automatic,
	DiskMB:                      configuration.Automatic,
	TempDir:                     "/tmp",
	RegistryPruningInterval:     time.Minute,
	ContainerInodeLimit:         200000,
	ContainerMaxCpuShares:       0,
	CachePath:                   "/tmp/cache",
	MaxCacheSizeInBytes:         10 * 1024 * 1024 * 1024,
	AllowPrivileged:             false,
	SkipCertVerify:              false,
	HealthyMonitoringInterval:   30 * time.Second,
	UnhealthyMonitoringInterval: 500 * time.Millisecond,
	ExportNetworkEnvVars:        false,
	ContainerOwnerName:          "executor",
	CreateWorkPoolSize:          defaultCreateWorkPoolSize,
	DeleteWorkPoolSize:          defaultDeleteWorkPoolSize,
	ReadWorkPoolSize:            defaultReadWorkPoolSize,
	MetricsWorkPoolSize:         defaultMetricsWorkPoolSize,
	HealthCheckWorkPoolSize:     defaultHealthCheckWorkPoolSize,
}

func Initialize(logger lager.Logger, config Configuration) (executor.Client, grouper.Members, error) {
	gardenClient := GardenClient.New(GardenConnection.New(config.GardenNetwork, config.GardenAddr))
	waitForGarden(logger, gardenClient)

	containersFetcher := &executorContainers{
		gardenClient: gardenClient,
		owner:        config.ContainerOwnerName,
	}

	destroyContainers(gardenClient, containersFetcher, logger)

	workDir := setupWorkDir(logger, config.TempDir)
	clock := clock.NewClock()

	transformer := initializeTransformer(
		logger,
		config.CachePath,
		workDir,
		config.MaxCacheSizeInBytes,
		maxConcurrentDownloads,
		maxConcurrentUploads,
		config.AllowPrivileged,
		config.SkipCertVerify,
		config.ExportNetworkEnvVars,
		clock,
	)

	hub := event.NewHub()

	gardenStore, err := gardenstore.NewGardenStore(
		gardenClient,
		config.ContainerOwnerName,
		config.ContainerMaxCpuShares,
		config.ContainerInodeLimit,
		config.HealthyMonitoringInterval,
		config.UnhealthyMonitoringInterval,
		transformer,
		clock,
		hub,
		config.HealthCheckWorkPoolSize,
	)
	if err != nil {
		return nil, grouper.Members{}, err
	}

	allocationStore := allocationstore.NewAllocationStore(clock, hub)

	workPoolSettings := executor.WorkPoolSettings{
		CreateWorkPoolSize:  config.CreateWorkPoolSize,
		DeleteWorkPoolSize:  config.DeleteWorkPoolSize,
		ReadWorkPoolSize:    config.ReadWorkPoolSize,
		MetricsWorkPoolSize: config.MetricsWorkPoolSize,
	}

	depotClientProvider, err := depot.NewClientProvider(
		fetchCapacity(logger, gardenClient, config),
		allocationStore,
		gardenStore,
		hub,
		keyed_lock.NewLockManager(),
		workPoolSettings,
	)
	if err != nil {
		return nil, grouper.Members{}, err
	}

	metricsLogger := logger.Session("metrics-reporter")
	containerMetricsLogger := logger.Session("container-metrics-reporter")

	return depotClientProvider.WithLogger(logger),
		grouper.Members{
			{"metrics-reporter", &metrics.Reporter{
				ExecutorSource: depotClientProvider.WithLogger(metricsLogger),
				Interval:       metricsReportInterval,
				Logger:         metricsLogger,
			}},
			{"hub-closer", closeHub(hub)},
			{"registry-pruner", allocationStore.RegistryPruner(logger, config.RegistryPruningInterval)},
			{"container-metrics-reporter", containermetrics.NewStatsReporter(
				containerMetricsLogger,
				containerMetricsReportInterval,
				clock,
				depotClientProvider.WithLogger(containerMetricsLogger),
			)},
		},
		nil
}

func ValidateExecutor(logger lager.Logger, config Configuration) bool {
	valid := true

	if config.ContainerMaxCpuShares == 0 {
		logger.Error("max-cpu-shares-invalid", nil)
		valid = false
	}

	if config.HealthyMonitoringInterval <= 0 {
		logger.Error("healthy-monitoring-interval-invalid", nil)
		valid = false
	}

	if config.UnhealthyMonitoringInterval <= 0 {
		logger.Error("unhealthy-monitoring-interval-invalid", nil)
		valid = false
	}

	return valid
}

func waitForGarden(logger lager.Logger, gardenClient GardenClient.Client) {
	err := gardenClient.Ping()

	for err != nil {
		logger.Error("failed-to-make-connection", err)
		time.Sleep(time.Second)
		err = gardenClient.Ping()
	}
}

func fetchCapacity(logger lager.Logger, gardenClient GardenClient.Client, config Configuration) executor.ExecutorResources {
	capacity, err := configuration.ConfigureCapacity(gardenClient, config.MemoryMB, config.DiskMB)
	if err != nil {
		logger.Error("failed-to-configure-capacity", err)
		os.Exit(1)
	}

	logger.Info("initial-capacity", lager.Data{
		"capacity": capacity,
	})

	return capacity
}

func destroyContainers(gardenClient garden.Client, containersFetcher *executorContainers, logger lager.Logger) {
	logger.Info("executor-fetching-containers-to-destroy")
	containers, err := containersFetcher.Containers()
	if err != nil {
		logger.Fatal("executor-failed-to-get-containers", err)
		return
	} else {
		logger.Info("executor-fetched-containers-to-destroy", lager.Data{"num-containers": len(containers)})
	}

	for _, container := range containers {
		logger.Info("executor-destroying-container", lager.Data{"container-handle": container.Handle()})
		err := gardenClient.Destroy(container.Handle())
		if err != nil {
			logger.Fatal("executor-failed-to-destroy-container", err, lager.Data{
				"handle": container.Handle(),
			})
		} else {
			logger.Info("executor-destroyed-stray-container", lager.Data{
				"handle": container.Handle(),
			})
		}
	}
}

func setupWorkDir(logger lager.Logger, tempDir string) string {
	workDir := filepath.Join(tempDir, "executor-work")

	err := os.RemoveAll(workDir)
	if err != nil {
		logger.Error("working-dir.cleanup-failed", err)
		os.Exit(1)
	}

	err = os.MkdirAll(workDir, 0755)
	if err != nil {
		logger.Error("working-dir.create-failed", err)
		os.Exit(1)
	}

	return workDir
}

func initializeTransformer(
	logger lager.Logger,
	cachePath, workDir string,
	maxCacheSizeInBytes uint64,
	maxConcurrentDownloads, maxConcurrentUploads uint,
	allowPrivileged bool,
	skipSSLVerification bool,
	exportNetworkEnvVars bool,
	clock clock.Clock,
) *transformer.Transformer {
	cache := cacheddownloader.New(cachePath, workDir, int64(maxCacheSizeInBytes), 10*time.Minute, int(math.MaxInt8), skipSSLVerification)
	uploader := uploader.New(10*time.Minute, skipSSLVerification, logger)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()

	return transformer.NewTransformer(
		cache,
		uploader,
		extractor,
		compressor,
		make(chan struct{}, maxConcurrentDownloads),
		make(chan struct{}, maxConcurrentUploads),
		workDir,
		allowPrivileged,
		exportNetworkEnvVars,
		clock,
	)
}

func closeHub(hub event.Hub) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		<-signals
		hub.Close()
		return nil
	})
}
