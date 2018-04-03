package containermetrics

import (
	"os"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/sonde-go/events"
)

var megabytesToBytes int = 1024 * 1024

type StatsReporter struct {
	logger lager.Logger

	interval       time.Duration
	clock          clock.Clock
	executorClient executor.Client
	metrics        atomic.Value

	metronClient          loggingclient.IngressClient
	enableContainerProxy  bool
	proxyMemoryAllocation float64
}

type cpuInfo struct {
	timeSpentInCPU time.Duration
	timeOfSample   time.Time
}

func NewStatsReporter(logger lager.Logger,
	interval time.Duration,
	clock clock.Clock,
	enableContainerProxy bool,
	additionalMemoryMB int,
	executorClient executor.Client,
	metronClient loggingclient.IngressClient,
) *StatsReporter {
	return &StatsReporter{
		logger: logger,

		interval:              interval,
		clock:                 clock,
		executorClient:        executorClient,
		metronClient:          metronClient,
		enableContainerProxy:  enableContainerProxy,
		proxyMemoryAllocation: float64(additionalMemoryMB * megabytesToBytes),
	}
}

func (reporter *StatsReporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := reporter.logger.Session("container-metrics-reporter")

	ticker := reporter.clock.NewTicker(reporter.interval)
	defer ticker.Stop()

	close(ready)

	cpuInfos := make(map[string]*cpuInfo)
	for {
		select {
		case signal := <-signals:
			logger.Info("signalled", lager.Data{"signal": signal.String()})
			return nil

		case now := <-ticker.C():
			cpuInfos = reporter.emitContainerMetrics(logger, cpuInfos, now)
		}
	}
}

func (reporter *StatsReporter) Metrics() map[string]*CachedContainerMetrics {
	if v := reporter.metrics.Load(); v != nil {
		return v.(map[string]*CachedContainerMetrics)
	}
	return nil
}

func (reporter *StatsReporter) emitContainerMetrics(logger lager.Logger, previousCPUInfos map[string]*cpuInfo, now time.Time) map[string]*cpuInfo {
	logger = logger.Session("tick")

	startTime := reporter.clock.Now()

	logger.Debug("started")
	defer func() {
		logger.Debug("done", lager.Data{
			"took": reporter.clock.Now().Sub(startTime).String(),
		})
	}()

	metrics, err := reporter.executorClient.GetBulkMetrics(logger)
	if err != nil {
		logger.Error("failed-to-get-all-metrics", err)
		return previousCPUInfos
	}

	logger.Debug("emitting", lager.Data{
		"total-containers": len(metrics),
		"get-metrics-took": reporter.clock.Now().Sub(startTime).String(),
	})

	containers, err := reporter.executorClient.ListContainers(logger)
	if err != nil {
		logger.Error("failed-to-fetch-containers", err)
		return previousCPUInfos
	}

	newCPUInfos := make(map[string]*cpuInfo)
	repMetricsMap := make(map[string]*CachedContainerMetrics)

	for _, container := range containers {
		guid := container.Guid
		metric := metrics[guid]

		previousCPUInfo := previousCPUInfos[guid]

		if reporter.enableContainerProxy && container.EnableContainerProxy {
			metric.MemoryUsageInBytes = uint64(float64(metric.MemoryUsageInBytes) * reporter.scaleMemory(container))
			metric.MemoryLimitInBytes = uint64(float64(metric.MemoryLimitInBytes) - reporter.proxyMemoryAllocation)
		}

		repMetrics, cpu := reporter.calculateAndSendMetrics(logger, metric.MetricsConfig, metric.ContainerMetrics, previousCPUInfo, now)
		if cpu != nil {
			newCPUInfos[guid] = cpu
		}

		if repMetrics != nil {
			repMetricsMap[guid] = repMetrics
		}
	}

	reporter.metrics.Store(repMetricsMap)
	return newCPUInfos
}

func (reporter *StatsReporter) calculateAndSendMetrics(
	logger lager.Logger,
	metricsConfig executor.MetricsConfig,
	containerMetrics executor.ContainerMetrics,
	previousInfo *cpuInfo,
	now time.Time,
) (*CachedContainerMetrics, *cpuInfo) {
	currentInfo, cpuPercent := calculateInfo(containerMetrics, previousInfo, now)

	if metricsConfig.Guid != "" {
		instanceIndex := int32(metricsConfig.Index)
		err := reporter.metronClient.SendAppMetrics(&events.ContainerMetric{
			ApplicationId:    &metricsConfig.Guid,
			InstanceIndex:    &instanceIndex,
			CpuPercentage:    &cpuPercent,
			MemoryBytes:      &containerMetrics.MemoryUsageInBytes,
			DiskBytes:        &containerMetrics.DiskUsageInBytes,
			MemoryBytesQuota: &containerMetrics.MemoryLimitInBytes,
			DiskBytesQuota:   &containerMetrics.DiskLimitInBytes,
		})

		if err != nil {
			logger.Error("failed-to-send-container-metrics", err, lager.Data{
				"metrics_guid":  metricsConfig.Guid,
				"metrics_index": metricsConfig.Index,
			})
		}
	}

	return &CachedContainerMetrics{
		MetricGUID:       metricsConfig.Guid,
		CPUUsageFraction: cpuPercent,
		DiskUsageBytes:   containerMetrics.DiskUsageInBytes,
		DiskQuotaBytes:   containerMetrics.DiskLimitInBytes,
		MemoryUsageBytes: containerMetrics.MemoryUsageInBytes,
		MemoryQuotaBytes: containerMetrics.MemoryLimitInBytes,
	}, &currentInfo
}

func calculateInfo(containerMetrics executor.ContainerMetrics, previousInfo *cpuInfo, now time.Time) (cpuInfo, float64) {
	currentInfo := cpuInfo{
		timeSpentInCPU: containerMetrics.TimeSpentInCPU,
		timeOfSample:   now,
	}

	var cpuPercent float64
	if previousInfo == nil {
		cpuPercent = 0.0
	} else {
		cpuPercent = computeCPUPercent(
			previousInfo.timeSpentInCPU,
			currentInfo.timeSpentInCPU,
			previousInfo.timeOfSample,
			currentInfo.timeOfSample,
		)
	}
	return currentInfo, cpuPercent
}

// scale from (0 - 100) * runtime.NumCPU()
func computeCPUPercent(timeSpentA, timeSpentB time.Duration, sampleTimeA, sampleTimeB time.Time) float64 {
	// divide change in time spent in CPU over time between samples.
	// result is out of 100 * runtime.NumCPU()
	//
	// don't worry about overflowing int64. it's like, 30 years.
	return float64((timeSpentB-timeSpentA)*100) / float64(sampleTimeB.UnixNano()-sampleTimeA.UnixNano())
}

func (reporter *StatsReporter) scaleMemory(container executor.Container) float64 {
	memFloat := float64(container.MemoryLimit)
	return (memFloat - reporter.proxyMemoryAllocation) / memFloat
}
