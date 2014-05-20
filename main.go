package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry-incubator/executor/configuration"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
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
	"tcp",
	"network mode for warden server (tcp, unix)",
)

var wardenAddr = flag.String(
	"wardenAddr",
	":7031",
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

var heartbeatInterval = flag.Duration(
	"heartbeatInterval",
	60*time.Second,
	"the interval, in seconds, between heartbeats for maintaining presence",
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

var containerInodeLimit = flag.Int(
	"containerInodeLimit",
	200000,
	"max number of inodes per container",
)

var containerMaxCpuShares = flag.Int(
	"containerMaxCpuShares",
	100,
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
	logger := steno.NewLogger("executor")

	wardenClient := WardenClient.New(&WardenConnection.Info{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	capacity, err := configuration.ConfigureCapacity(wardenClient, *memoryMBFlag, *diskMBFlag)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	logger.Infof("Initial Capacity: %s", capacity)

	if *containerMaxCpuShares <= 0 {
		logger.Error("valid maximum container cpu shares must be specified on startup!")
		os.Exit(1)
	}

	cache := cacheddownloader.New(*cachePath, *tempDir, *maxCacheSizeInBytes, 10*time.Minute)
	uploader := uploader.New(10*time.Minute, logger)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()
	logStreamerFactory := log_streamer_factory.New(
		*loggregatorServer,
		*loggregatorSecret,
	)

	transformer := Transformer.NewTransformer(
		logStreamerFactory,
		cache,
		uploader,
		extractor,
		compressor,
		logger,
		*tempDir,
	)

	logger.Info("executor.starting")

	executor := executor.New(
		*listenAddr,
		*containerOwnerName,
		uint64(*containerMaxCpuShares),
		capacity,
		wardenClient,
		transformer,
		*drainTimeout,
		logger,
	)

	executorSignals := make(chan os.Signal, 1)
	signal.Notify(executorSignals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	executorReady := make(chan struct{})

	go func() {
		<-executorReady
		logger.Info("executor.started")
	}()

	err = executor.Run(executorSignals, executorReady)

	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
