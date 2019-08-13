package containermetrics

import (
	"os"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

type MetricsHandler interface {
	Handle(logger lager.Logger, container executor.Container, metric executor.Metrics, timeStamp time.Time) error
}

type StatsReporter struct {
	logger lager.Logger

	interval        time.Duration
	clock           clock.Clock
	executorClient  executor.Client
	metricsCache    *atomic.Value
	metricsHandlers []MetricsHandler
}

func NewStatsReporter(logger lager.Logger,
	interval time.Duration,
	clock clock.Clock,
	executorClient executor.Client,
	metricsCache *atomic.Value,
	metricsHandlers ...MetricsHandler,
) *StatsReporter {
	return &StatsReporter{
		logger: logger,

		interval:        interval,
		clock:           clock,
		executorClient:  executorClient,
		metricsCache:    metricsCache,
		metricsHandlers: metricsHandlers,
	}
}

func (reporter *StatsReporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := reporter.logger.Session("container-metrics-reporter")

	ticker := reporter.clock.NewTicker(reporter.interval)
	defer ticker.Stop()

	close(ready)

	for {
		select {
		case signal := <-signals:
			logger.Info("signalled", lager.Data{"signal": signal.String()})
			return nil

		case now := <-ticker.C():
			reporter.fetchMetrics(logger, now, reporter.metricsHandlers...)
		}
	}
}

func (reporter *StatsReporter) Metrics() map[string]*CachedContainerMetrics {
	if v := reporter.metricsCache.Load(); v != nil {
		return v.(map[string]*CachedContainerMetrics)
	}
	return nil
}

func (reporter *StatsReporter) fetchMetrics(logger lager.Logger, timeStamp time.Time, handlers ...MetricsHandler) {
	logger = logger.Session("tick")

	startTime := reporter.clock.Now()

	logger.Debug("started")
	defer func() {
		logger.Debug("done", lager.Data{
			"took": reporter.clock.Now().Sub(startTime).String(),
		})
	}()

	metricsCache, err := reporter.executorClient.GetBulkMetrics(logger)
	if err != nil {
		logger.Error("failed-to-get-all-metrics", err)
		return
	}

	logger.Debug("emitting", lager.Data{
		"total-containers": len(metricsCache),
		"get-metrics-took": reporter.clock.Now().Sub(startTime).String(),
	})

	containers, err := reporter.executorClient.ListContainers(logger)
	if err != nil {
		logger.Error("failed-to-fetch-containers", err)
		return
	}

	for _, container := range containers {
		metric := metricsCache[container.Guid]
		for _, handler := range handlers {
			err := handler.Handle(logger, container, metric, timeStamp)
			if err != nil {
				logger.Error("failed-to-handle-metric", err, lager.Data{"container": container, "metric": metric})
			}
		}
	}
}
