package main

import (
	"flag"
	"os"
	"time"

	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry-incubator/executor/registry"
	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/cloudfoundry/gunk/group_runner"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/sigmon"

	cf_debug_server "github.com/cloudfoundry-incubator/cf-debug-server"
	"github.com/cloudfoundry-incubator/executor/configuration"
	"github.com/cloudfoundry-incubator/executor/server"
	Transformer "github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader"
	_ "github.com/cloudfoundry/dropsonde/autowire"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
	"github.com/pivotal-golang/lager"
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:1700",
	"host:port to serve API requests on")

var containerOwnerName = flag.String(
	"containerOwnerName",
	"executor",
	"name to track containers created by this executor; they will be reaped on start",
)

var wardenNetwork = flag.String(
	"wardenNetwork",
	"unix",
	"network mode for warden server (tcp, unix)",
)

var wardenAddr = flag.String(
	"wardenAddr",
	"/tmp/warden.sock",
	"network address for warden server",
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

var containerInodeLimit = flag.Uint(
	"containerInodeLimit",
	200000,
	"max number of inodes per container",
)

var containerMaxCpuShares = flag.Int(
	"containerMaxCpuShares",
	0,
	"cpu shares allocatable to a container",
)

var cachePath = flag.String(
	"cachePath",
	"/tmp/cache",
	"location to cache assets",
)

var maxCacheSizeInBytes = flag.Int64(
	"maxCacheSizeInBytes",
	10*1024*1024*1024,
	"maximum size of the cache (in bytes) - you should include a healthy amount of overhead",
)

func main() {
	flag.Parse()

	logger := cf_lager.New("executor")

	if *containerMaxCpuShares <= 0 {
		logger.Error("max-cpu-shares-invalid", nil)
		os.Exit(1)
	}

	logger.Info("starting")

	wardenClient := WardenClient.New(WardenConnection.New(*wardenNetwork, *wardenAddr))
	waitForWarden(logger, wardenClient)
	destroyContainers(wardenClient, logger)
	capacity := fetchCapacity(logger, wardenClient)

	reg := registry.New(capacity, timeprovider.NewTimeProvider())

	depotClient := depot.NewClient(
		*containerOwnerName,
		uint64(*containerMaxCpuShares),
		uint64(*containerInodeLimit),
		wardenClient,
		reg,
		initializeTransformer(logger),
		logger,
	)

	apiServer := &server.Server{
		Address:     *listenAddr,
		Logger:      logger,
		DepotClient: depotClient,
	}

	pruner := registry.NewPruner(reg, timeprovider.NewTimeProvider(), *registryPruningInterval, logger)

	group := group_runner.New([]group_runner.Member{
		{"registry-pruner", pruner},
		{"api-server", apiServer},
	})

	cf_debug_server.Run()

	monitor := ifrit.Envoke(sigmon.New(group))

	logger.Info("started")

	err := <-monitor.Wait()
	if err != nil {
		logger.Error("exited-with-failure", err)
		os.Exit(1)
	}

	logger.Info("exited")
}

func initializeTransformer(logger lager.Logger) *Transformer.Transformer {
	cache := cacheddownloader.New(*cachePath, *tempDir, *maxCacheSizeInBytes, 10*time.Minute)
	uploader := uploader.New(10 * time.Minute)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()

	os.Setenv("LOGGREGATOR_SHARED_SECRET", *loggregatorSecret)

	logEmitter, err := logemitter.NewEmitter(
		*loggregatorServer,
		"",
		"",
		false,
	)
	if err != nil {
		panic(err)
	}

	return Transformer.NewTransformer(
		logEmitter,
		cache,
		uploader,
		extractor,
		compressor,
		logger,
		*tempDir,
	)
}

func waitForWarden(logger lager.Logger, wardenClient WardenClient.Client) {
	err := wardenClient.Ping()

	for err != nil {
		logger.Error("failed-to-make-connection", err)
		time.Sleep(time.Second)
		err = wardenClient.Ping()
	}
}

func fetchCapacity(logger lager.Logger, wardenClient WardenClient.Client) registry.Capacity {
	capacity, err := configuration.ConfigureCapacity(wardenClient, *memoryMBFlag, *diskMBFlag)
	if err != nil {
		logger.Error("failed-to-configure-capacity", err)
		os.Exit(1)
	}

	logger.Info("initial-capacity", lager.Data{
		"capacity": capacity,
	})

	return capacity
}

func destroyContainers(wardenClient warden.Client, logger lager.Logger) {
	containers, err := wardenClient.Containers(warden.Properties{
		"owner": *containerOwnerName,
	})

	if err != nil {
		logger.Fatal("failed-to-get-containers", err)
		return
	}

	for _, container := range containers {
		err := wardenClient.Destroy(container.Handle())
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
