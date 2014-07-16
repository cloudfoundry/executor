package main

import (
	"flag"
	"log"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/registry"
	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/loggregatorlib/emitter"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"

	"github.com/cloudfoundry-incubator/executor/configuration"
	"github.com/cloudfoundry-incubator/executor/server"
	Transformer "github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/cacheddownloader"
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

var logLevel = flag.String(
	"logLevel",
	"info",
	"the logging level (none, fatal, error, warn, info, debug, debug1, debug2, all)",
)

var syslogName = flag.String(
	"syslogName",
	"",
	"syslog name",
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

var containerInodeLimit = flag.Int(
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

	logger := initializeLogger()

	if *containerMaxCpuShares <= 0 {
		logger.Error("valid maximum container cpu shares must be specified on startup!")
		os.Exit(1)
	}

	wardenClient, capacity := initializeWardenClient(logger)
	transformer := initializeTransformer(logger)
	reg := registry.New(capacity, timeprovider.NewTimeProvider())

	logger.Info("executor.starting")

	runWaitGroup := new(sync.WaitGroup)
	runCanceller := make(chan struct{})

	executor := executor.New(
		*containerOwnerName,
		wardenClient,
		*drainTimeout,
		runWaitGroup,
		runCanceller,
		logger,
	)

	apiServer := &server.Server{
		Address:               *listenAddr,
		Registry:              reg,
		WardenClient:          wardenClient,
		ContainerOwnerName:    *containerOwnerName,
		ContainerMaxCPUShares: uint64(*containerMaxCpuShares),
		Transformer:           transformer,
		Logger:                logger,
		RunWaitGroup:          runWaitGroup,
		RunCanceller:          runCanceller,
	}

	pruner := registry.NewPruner(reg, timeprovider.NewTimeProvider(), *registryPruningInterval)

	processGroup := grouper.EnvokeGroup(grouper.RunGroup{
		"executor":        executor,
		"registry-pruner": pruner,
		"api-server":      apiServer,
	})

	monitor := ifrit.Envoke(sigmon.New(processGroup, syscall.SIGUSR1))
	exitChan := processGroup.Exits()

	logger.Info("executor.started")

	for {
		select {
		case member := <-exitChan:
			if member.Error != nil || member.Name == "executor" {
				logger.Infof("executor.member.%s.exited", member.Name)
				monitor.Signal(syscall.SIGTERM)
			}

		case err := <-monitor.Wait():
			if err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

func initializeLogger() *steno.Logger {
	l, err := steno.GetLogLevel(*logLevel)
	if err != nil {
		log.Fatalf("Invalid loglevel: %s\n", *logLevel)
	}

	stenoConfig := steno.Config{
		Level: l,
		Sinks: []steno.Sink{steno.NewIOSink(os.Stdout)},
	}

	if *syslogName != "" {
		stenoConfig.Sinks = append(stenoConfig.Sinks, steno.NewSyslogSink(*syslogName))
	}

	steno.Init(&stenoConfig)
	return steno.NewLogger("executor")
}

func initializeWardenClient(logger *steno.Logger) (WardenClient.Client, registry.Capacity) {
	wardenClient := WardenClient.New(WardenConnection.New(*wardenNetwork, *wardenAddr))

	capacity, err := configuration.ConfigureCapacity(wardenClient, *memoryMBFlag, *diskMBFlag)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	logger.Infof("Initial Capacity: %s", capacity.String())

	return wardenClient, capacity
}

func initializeTransformer(logger *steno.Logger) *Transformer.Transformer {
	cache := cacheddownloader.New(*cachePath, *tempDir, *maxCacheSizeInBytes, 10*time.Minute)
	uploader := uploader.New(10*time.Minute, logger)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()

	logEmitter, _ := emitter.NewEmitter(
		*loggregatorServer,
		"",
		"",
		*loggregatorSecret,
		nil,
	)

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
