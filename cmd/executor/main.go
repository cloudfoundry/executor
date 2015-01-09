package main

import (
	"flag"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/depot/metrics"
	garden "github.com/cloudfoundry-incubator/garden/api"
	GardenClient "github.com/cloudfoundry-incubator/garden/client"
	GardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"

	cf_debug_server "github.com/cloudfoundry-incubator/cf-debug-server"
	"github.com/cloudfoundry-incubator/executor/cmd/executor/configuration"

	"github.com/cloudfoundry-incubator/executor/depot/tallyman"
	"github.com/cloudfoundry-incubator/executor/depot/transformer"
	"github.com/cloudfoundry-incubator/executor/depot/uploader"
	"github.com/cloudfoundry-incubator/executor/http/server"
	"github.com/cloudfoundry/dropsonde"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:1700",
	"host:port to serve API requests on",
)

var gardenNetwork = flag.String(
	"gardenNetwork",
	"unix",
	"network mode for garden server (tcp, unix)",
)

var gardenAddr = flag.String(
	"gardenAddr",
	"/tmp/garden.sock",
	"network address for garden server",
)

var memoryMBFlag = flag.String(
	"memoryMB",
	configuration.Automatic,
	"the amount of memory the executor has available in megabytes",
)

var diskMBFlag = flag.String(
	"diskMB",
	configuration.Automatic,
	"the amount of disk the executor has available in megabytes",
)

var tempDir = flag.String(
	"tempDir",
	"/tmp",
	"location to store temporary assets",
)

var drainTimeout = flag.Duration(
	"drainTimeout",
	15*time.Minute,
	"time to give running tasks to drain before exiting",
)

var registryPruningInterval = flag.Duration(
	"pruneInterval",
	time.Minute,
	"amount of time during which a container can remain in the allocated state",
)

var containerInodeLimit = flag.Uint64(
	"containerInodeLimit",
	200000,
	"max number of inodes per container",
)

var containerMaxCpuShares = flag.Uint64(
	"containerMaxCpuShares",
	0,
	"cpu shares allocatable to a container",
)

var cachePath = flag.String(
	"cachePath",
	"/tmp/cache",
	"location to cache assets",
)

var maxCacheSizeInBytes = flag.Uint64(
	"maxCacheSizeInBytes",
	10*1024*1024*1024,
	"maximum size of the cache (in bytes) - you should include a healthy amount of overhead",
)

var allowPrivileged = flag.Bool(
	"allowPrivileged",
	false,
	"allow creation of privileged containers",
)

var skipCertVerify = flag.Bool(
	"skipCertVerify",
	false,
	"skip SSL certificate verification",
)

var healthyMonitoringInterval = flag.Duration(
	"healthyMonitoringInterval",
	30*time.Second,
	"interval on which to check healthy containers",
)

var unhealthyMonitoringInterval = flag.Duration(
	"unhealthyMonitoringInterval",
	500*time.Millisecond,
	"interval on which to check unhealthy containers",
)

var exportNetworkEnvVars = flag.Bool(
	"exportNetworkEnvVars",
	false,
	"export network environment variables into container (e.g. INSTANCE_IP, INSTANCE_PORT)",
)

const (
	containerOwnerName     = "executor"
	dropsondeDestination   = "localhost:3457"
	dropsondeOrigin        = "executor"
	maxConcurrentDownloads = 5
	maxConcurrentUploads   = 5
	metricsReportInterval  = 1 * time.Minute
)

func main() {
	cf_debug_server.AddFlags(flag.CommandLine)
	flag.Parse()

	logger := cf_lager.New("executor")

	if !validate(logger) {
		os.Exit(1)
	}

	initializeDropsonde(logger)

	logger.Info("starting")

	gardenClient := GardenClient.New(GardenConnection.New(*gardenNetwork, *gardenAddr))
	waitForGarden(logger, gardenClient)

	containersFetcher := &executorContainers{
		gardenClient: gardenClient,
		owner:        containerOwnerName,
	}

	destroyContainers(gardenClient, containersFetcher, logger)

	workDir := setupWorkDir(logger, *tempDir)

	transformer := initializeTransformer(
		logger,
		*cachePath,
		workDir,
		*maxCacheSizeInBytes,
		maxConcurrentDownloads,
		maxConcurrentUploads,
		*allowPrivileged,
		*skipCertVerify,
		*exportNetworkEnvVars,
	)

	hub := event.NewHub()
	timeProvider := timeprovider.NewTimeProvider()
	tallyman := tallyman.NewTallyman(timeProvider)

	gardenStore := gardenstore.NewGardenStore(
		gardenClient,
		containerOwnerName,
		*containerMaxCpuShares,
		*containerInodeLimit,
		*healthyMonitoringInterval,
		*unhealthyMonitoringInterval,
		transformer,
		timeProvider,
		hub,
	)

	depotClientProvider := depot.NewClientProvider(
		fetchCapacity(logger, gardenClient),
		tallyman,
		gardenStore,
		hub,
	)

	metricsLogger := logger.Session("metrics-reporter")

	members := grouper.Members{
		{"api-server", &server.Server{
			Address:             *listenAddr,
			Logger:              logger,
			DepotClientProvider: depotClientProvider,
		}},
		{"metrics-reporter", &metrics.Reporter{
			ExecutorSource: depotClientProvider.WithLogger(metricsLogger),
			Interval:       metricsReportInterval,
			Logger:         metricsLogger,
		}},
		{"hub-drainer", drainHub(hub)},
		{"registry-pruner", tallyman.RegistryPruner(logger, *registryPruningInterval)},
	}

	if dbgAddr := cf_debug_server.DebugAddress(flag.CommandLine); dbgAddr != "" {
		members = append(grouper.Members{
			{"debug-server", cf_debug_server.Runner(dbgAddr)},
		}, members...)
	}

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group))

	logger.Info("started")

	err := <-monitor.Wait()
	if err != nil {
		logger.Error("exited-with-failure", err)
		os.Exit(1)
	}

	logger.Info("exited")
}

func validate(logger lager.Logger) bool {
	valid := true

	if *containerMaxCpuShares == 0 {
		logger.Error("max-cpu-shares-invalid", nil)
		valid = false
	}

	if *healthyMonitoringInterval <= 0 {
		logger.Error("healthy-monitoring-interval-invalid", nil)
		valid = false
	}

	if *unhealthyMonitoringInterval <= 0 {
		logger.Error("unhealthy-monitoring-interval-invalid", nil)
		valid = false
	}

	return valid
}

func drainHub(hub event.Hub) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		<-signals
		hub.Close()
		return nil
	})
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

func initializeDropsonde(logger lager.Logger) {
	err := dropsonde.Initialize(dropsondeDestination, dropsondeOrigin)
	if err != nil {
		logger.Error("failed to initialize dropsonde: %v", err)
	}
}

func initializeTransformer(
	logger lager.Logger,
	cachePath, workDir string,
	maxCacheSizeInBytes uint64,
	maxConcurrentDownloads, maxConcurrentUploads uint,
	allowPrivileged bool,
	skipSSLVerification bool,
	exportNetworkEnvVars bool,
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
	)
}

func waitForGarden(logger lager.Logger, gardenClient GardenClient.Client) {
	err := gardenClient.Ping()

	for err != nil {
		logger.Error("failed-to-make-connection", err)
		time.Sleep(time.Second)
		err = gardenClient.Ping()
	}
}

func fetchCapacity(logger lager.Logger, gardenClient GardenClient.Client) executor.ExecutorResources {
	capacity, err := configuration.ConfigureCapacity(gardenClient, *memoryMBFlag, *diskMBFlag)
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
