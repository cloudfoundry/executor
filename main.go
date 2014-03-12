package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	"github.com/vito/gordon"

	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/linuxplugin"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/run_once_transformer"
	"github.com/cloudfoundry-incubator/executor/runoncehandler"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	"github.com/cloudfoundry-incubator/executor/uploader"
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

var etcdCluster = flag.String(
	"etcdCluster",
	"http://127.0.0.1:4001",
	"comma-separated list of etcd addresses (http://ip:port)",
)

var logLevel = flag.String(
	"logLevel",
	"info",
	"the logging level (none, fatal, error, warn, info, debug, debug1, debug2, all)",
)

var memoryMB = flag.Int(
	"memoryMB",
	0,
	"the amount of memory the executor has available in megabytes",
)

var diskMB = flag.Int(
	"diskMB",
	0,
	"the amount of disk the executor has available in megabytes",
)

var registrySnapshotFile = flag.String(
	"registrySnapshotFile",
	"registry_snapshot",
	"the location, on disk, where the task registry snapshot should be stored",
)

var convergenceInterval = flag.Duration(
	"convergenceInterval",
	30*time.Second,
	"the interval, in seconds, between convergences",
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

var stack = flag.String(
	"stack",
	"default",
	"the executor stack",
)

var timeToClaimRunOnce = flag.Duration(
	"timeToClaimRunOnce",
	30*time.Minute,
	"unclaimed run onces are marked as failed, after this time (in seconds)",
)

var containerInodeLimit = flag.Int(
	"containerInodeLimit",
	200000,
	"max number of inodes per container",
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

	steno.Init(&stenoConfig)
	logger := steno.NewLogger("executor")

	etcdAdapter := etcdstoreadapter.NewETCDStoreAdapter(
		strings.Split(*etcdCluster, ","),
		workerpool.NewWorkerPool(10),
	)

	bbs := Bbs.New(etcdAdapter)
	err = etcdAdapter.Connect()
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "failed to get etcdAdapter to connect")
		os.Exit(1)
	}

	wardenClient := gordon.NewClient(&gordon.ConnectionInfo{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	err = wardenClient.Connect()
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err,
		}, "warden is not up!")
		os.Exit(1)
	}

	if *memoryMB <= 0 || *diskMB <= 0 {
		logger.Error("valid memory and disk capacity must be specified on startup!")
		os.Exit(1)
	}

	taskRegistry, err := taskregistry.LoadTaskRegistryFromDisk(*stack, *registrySnapshotFile, *memoryMB, *diskMB)
	if err != nil {
		switch err {
		case taskregistry.ErrorRegistrySnapshotHasInvalidJSON:
			logger.Error("corrupt registry snapshot detected.  aborting!")
			os.Exit(1)
		case taskregistry.ErrorNotEnoughMemoryWhenLoadingSnapshot:
			logger.Error("memory requirements in snapshot exceed the configured memory limit.  aborting!")
			os.Exit(1)
		case taskregistry.ErrorNotEnoughDiskWhenLoadingSnapshot:
			logger.Error("disk requirements in snapshot exceed the configured memory limit.  aborting!")
			os.Exit(1)
		case taskregistry.ErrorRegistrySnapshotDoesNotExist:
			logger.Info("Didn't find snapshot.  Creating new registry.")
			taskRegistry = taskregistry.NewTaskRegistry(*stack, *registrySnapshotFile, *memoryMB, *diskMB)
		default:
			logger.Errorf("woah, woah, woah!  what happened with the snapshot?: %s", err.Error())
			os.Exit(1)
		}
	}

	executor := executor.New(bbs, logger)
	err = executor.MaintainPresence(*heartbeatInterval)
	if err != nil {
		logger.Errorf("failed to start maintaining presence: %s", err.Error())
		os.Exit(1)
	}

	logger.Infof("Starting executor: ID=%s, stack=%s", executor.ID(), *stack)

	signals := make(chan os.Signal, 1)

	go func() {
		<-signals

		err := taskRegistry.WriteToDisk()
		if err != nil {
			logger.Errord(
				map[string]interface{}{
					"error":            err,
					"snapshotLocation": *registrySnapshotFile,
				},
				"executor.snapshot.write-failed",
			)

			os.Exit(1)
		} else {
			logger.Debugd(
				map[string]interface{}{
					"snapshotLocation": *registrySnapshotFile,
				},
				"executor.snapshot.saved",
			)
		}

		os.Exit(0)
	}()

	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	linuxPlugin := linuxplugin.New()
	downloader := downloader.New(10*time.Minute, logger)
	uploader := uploader.New(10*time.Minute, logger)

	logStreamerFactory := log_streamer_factory.New(
		*loggregatorServer,
		*loggregatorSecret,
	)

	transformer := run_once_transformer.NewRunOnceTransformer(
		logStreamerFactory,
		downloader,
		uploader,
		linuxPlugin,
		wardenClient,
		logger,
		*tempDir,
	)

	runOnceHandler := runoncehandler.New(
		bbs,
		wardenClient,
		taskRegistry,
		transformer,
		logStreamerFactory,
		logger,
		*containerInodeLimit,
	)

	executor.ConvergeRunOnces(*convergenceInterval, *timeToClaimRunOnce)

	ready := make(chan bool, 1)

	go func() {
		<-ready
		logger.Infof("Watching for RunOnces!")
	}()

	err = executor.Handle(runOnceHandler, ready)

	if err != nil {
		logger.Errord(
			map[string]interface{}{
				"error": err.Error(),
			},
			"executor.run-once-handling.failed",
		)

		os.Exit(1)
	}
}
