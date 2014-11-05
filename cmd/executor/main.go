package main

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/depot/event"
	"github.com/cloudfoundry-incubator/executor/depot/metrics"
	"github.com/cloudfoundry-incubator/executor/depot/store"
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
	_ "github.com/cloudfoundry/dropsonde/autowire"
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/timer"
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:1700",
	"host:port to serve API requests on",
)

var containerOwnerName = flag.String(
	"containerOwnerName",
	"executor",
	"name to track containers created by this executor; they will be reaped on start",
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

var loggregatorServer = flag.String(
	"loggregatorServer",
	"",
	"loggregator server to emit logs to",
)

var loggregatorSecret = flag.String(
	"loggregatorSecret",
	"",
	"secret for the loggregator server",
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

var metricsReportInterval = flag.Duration(
	"metricsReportInterval",
	1*time.Minute,
	"interval on which to report metrics",
)

var maxConcurrentDownloads = flag.Uint(
	"maxConcurrentDownloads",
	20,
	"maximum in-flight downloads",
)

var gardenSyncInterval = flag.Duration(
	"gardenSyncInterval",
	30*time.Second,
	"interval on which to poll Garden's containers to ensure usage is in sync",
)

func main() {
	flag.Parse()

	logger := cf_lager.New("executor")

	if *containerMaxCpuShares == 0 {
		logger.Error("max-cpu-shares-invalid", nil)
		os.Exit(1)
	}

	logger.Info("starting")

	cf_debug_server.Run()

	gardenClient := GardenClient.New(GardenConnection.New(*gardenNetwork, *gardenAddr))
	waitForGarden(logger, gardenClient)

	containersFetcher := &executorContainers{
		gardenClient: gardenClient,
		owner:        *containerOwnerName,
	}

	destroyContainers(gardenClient, containersFetcher, logger)

	workDir := setupWorkDir(logger, *tempDir)

	transformer := initializeTransformer(
		logger,
		*cachePath,
		workDir,
		*maxCacheSizeInBytes,
		*maxConcurrentDownloads,
	)

	tallyman := tallyman.NewTallyman()

	gardenStore, allocationStore := initializeStores(
		logger,
		gardenClient,
		transformer,
		*containerOwnerName,
		*containerMaxCpuShares,
		*containerInodeLimit,
		*loggregatorServer,
		*loggregatorSecret,
		*registryPruningInterval,
		tallyman,
	)

	depotClient := depot.NewClient(
		fetchCapacity(logger, gardenClient),
		gardenStore,
		allocationStore,
		tallyman,
		event.NewHub(),
		logger,
	)

	group := grouper.NewOrdered(os.Interrupt, grouper.Members{
		{"api-server", &server.Server{
			Address:     *listenAddr,
			Logger:      logger,
			DepotClient: depotClient,
		}},
		{"metrics-reporter", &metrics.Reporter{
			ExecutorSource: depotClient,
			Interval:       *metricsReportInterval,
			Logger:         logger.Session("metrics-reporter"),
		}},
		{"garden-syncer", gardenStore.TrackContainers(*gardenSyncInterval)},
	})

	monitor := ifrit.Envoke(sigmon.New(group))

	logger.Info("started")

	err := <-monitor.Wait()
	if err != nil {
		logger.Error("exited-with-failure", err)
		os.Exit(1)
	}

	logger.Info("exited")
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

func initializeStores(
	logger lager.Logger,
	gardenClient garden.Client,
	transformer *transformer.Transformer,
	containerOwnerName string,
	containerMaxCpuShares uint64,
	containerInodeLimit uint64,
	loggregatorServer string,
	loggregatorSecret string,
	registryPruningInterval time.Duration,
	tallyman *tallyman.Tallyman,
) (*store.GardenStore, *store.AllocationStore) {
	os.Setenv("LOGGREGATOR_SHARED_SECRET", loggregatorSecret)
	logEmitter, err := logemitter.NewEmitter(loggregatorServer, "", "", false)
	if err != nil {
		panic(err)
	}

	gardenStore := store.NewGardenStore(
		logger.Session("garden-store"),
		gardenClient,
		containerOwnerName,
		containerMaxCpuShares,
		containerInodeLimit,
		logEmitter,
		transformer,
		timer.NewTimer(),
		tallyman,
	)

	allocationStore := store.NewAllocationStore(
		timeprovider.NewTimeProvider(),
		registryPruningInterval,
		tallyman,
	)

	return gardenStore, allocationStore
}

func initializeTransformer(
	logger lager.Logger,
	cachePath, workDir string,
	maxCacheSizeInBytes uint64,
	maxConcurrentDownloads uint,
) *transformer.Transformer {
	cache := cacheddownloader.New(cachePath, workDir, int64(maxCacheSizeInBytes), 10*time.Minute, int(maxConcurrentDownloads))
	uploader := uploader.New(10*time.Minute, logger)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()

	return transformer.NewTransformer(
		cache,
		uploader,
		extractor,
		compressor,
		logger,
		workDir,
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
	containers, err := containersFetcher.Containers()
	if err != nil {
		logger.Fatal("failed-to-get-containers", err)
		return
	}

	for _, container := range containers {
		err := gardenClient.Destroy(container.Handle())
		if err != nil {
			logger.Fatal("failed-to-destroy-container", err, lager.Data{
				"handle": container.Handle(),
			})
		} else {
			logger.Info("destroyed-stray-container", lager.Data{
				"handle": container.Handle(),
			})
		}
	}
}
