package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"
	"github.com/vito/gordon"
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

var etcdMachines = flag.String(
	"etcdMachines",
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
	logger := steno.NewLogger("main")

	etcdAdapter := etcdstoreadapter.NewETCDStoreAdapter(
		strings.Split(*etcdMachines, ","),
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

	taskRegistry, err := taskregistry.LoadTaskRegistryFromDisk(*registrySnapshotFile, *memoryMB, *diskMB)
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
			taskRegistry = taskregistry.NewTaskRegistry(*registrySnapshotFile, *memoryMB, *diskMB)
		default:
			logger.Errorf("woah, woah, woah.  what happened with the snapshot?", err.Error())
			os.Exit(1)
		}
	}
	executor := executor.New(bbs, wardenClient, taskRegistry)

	executor.HandleRunOnces()
	logger.Infof("Watching for RunOnces!")

	executor.ConvergeRunOnces(30 * time.Second)
	logger.Infof("Converging RunOnces!")

	select {}
}
