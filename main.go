package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/gordon"
	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	"github.com/cloudfoundry/storeadapter/workerpool"

	"github.com/cloudfoundry-incubator/executor/downloader"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/executor/task_handler"
	"github.com/cloudfoundry-incubator/executor/task_registry"
	"github.com/cloudfoundry-incubator/executor/task_transformer"
	"github.com/cloudfoundry-incubator/executor/uploader"
	"github.com/pivotal-golang/archiver/compressor"
	"github.com/pivotal-golang/archiver/extractor"
)

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

var syslogName = flag.String(
	"syslogName",
	"",
	"syslog name",
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
	"",
	"the executor stack - must be specified",
)

var timeToClaimTask = flag.Duration(
	"timeToClaimTask",
	30*time.Minute,
	"unclaimed run onces are marked as failed, after this time (in seconds)",
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
	0,
	"cpu shares allocatable to a container",
)

func main() {
	flag.Parse()

	if *stack == "" {
		log.Fatalf("A stack must be specified")
	}

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

	etcdAdapter := etcdstoreadapter.NewETCDStoreAdapter(
		strings.Split(*etcdCluster, ","),
		workerpool.NewWorkerPool(10),
	)

	bbs := Bbs.New(etcdAdapter, timeprovider.NewTimeProvider())
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

	if *containerMaxCpuShares <= 0 {
		logger.Error("valid maximum container cpu shares must be specified on startup!")
		os.Exit(1)
	}

	executor := executor.New(bbs, *drainTimeout, logger)

	downloader := downloader.New(10*time.Minute, logger)
	uploader := uploader.New(10*time.Minute, logger)
	extractor := extractor.NewDetectable()
	compressor := compressor.NewTgz()
	taskRegistry := task_registry.NewTaskRegistry(*stack, *memoryMB, *diskMB)

	logStreamerFactory := log_streamer_factory.New(
		*loggregatorServer,
		*loggregatorSecret,
	)

	transformer := task_transformer.NewTaskTransformer(
		logStreamerFactory,
		downloader,
		uploader,
		extractor,
		compressor,
		wardenClient,
		logger,
		*tempDir,
	)

	taskHandler := task_handler.New(
		bbs,
		wardenClient,
		*containerOwnerName,
		taskRegistry,
		transformer,
		logStreamerFactory,
		logger,
		*containerInodeLimit,
		*containerMaxCpuShares,
	)

	maintaining := make(chan error, 1)
	maintainPresenceErrors := make(chan error, 1)

	err = taskHandler.Cleanup()
	if err != nil {
		logger.Errord(
			map[string]interface{}{
				"error": err.Error(),
			},
			"executor.cleanup.failed",
		)
		os.Exit(1)
	}

	logger.Infod(
		map[string]interface{}{
			"id":    executor.ID(),
			"stack": *stack,
		},
		"executor.starting",
	)

	go executor.MaintainPresence(*heartbeatInterval, maintaining, maintainPresenceErrors)

	err = <-maintaining
	if err != nil {
		logger.Errorf("failed to start maintaining presence: %s", err.Error())
		os.Exit(1)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	go executor.ConvergeTasks(*convergenceInterval, *timeToClaimTask)

	handling := make(chan bool)

	go executor.Handle(taskHandler, handling)

	<-handling
	logger.Infof("executor.started")

	select {
	case err := <-maintainPresenceErrors:
		executor.Stop()

		logger.Errord(
			map[string]interface{}{
				"error": err.Error(),
			},
			"executor.run-once-handling.failed",
		)

		os.Exit(1)

	case sig := <-signals:
		go func() {
			for sig := range signals {
				logger.Infod(
					map[string]interface{}{
						"signal": sig.String(),
					},
					"executor.signal.ignored",
				)
			}
		}()

		if sig == syscall.SIGUSR1 {
			logger.Infod(
				map[string]interface{}{
					"timeout": (*drainTimeout).String(),
				},
				"executor.draining",
			)

			executor.Drain()
		}

		stoppingAt := time.Now()

		logger.Info("executor.stopping")

		executor.Stop()

		logger.Infod(
			map[string]interface{}{
				"took": time.Since(stoppingAt).String(),
			},
			"executor.stopped",
		)

		os.Exit(0)
	}
}
