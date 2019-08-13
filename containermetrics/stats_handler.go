package containermetrics

import (
	"strconv"
	"sync/atomic"
	"time"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

type cpuInfo struct {
	timeSpentInCPU time.Duration
	timeOfSample   time.Time
}

type StatsHandler struct {
	cpuInfos              map[string]*cpuInfo
	repMetricsMap         map[string]*CachedContainerMetrics
	metronClient          loggingclient.IngressClient
	enableContainerProxy  bool
	proxyMemoryAllocation float64
	metricsCache          *atomic.Value
}

func NewStatsHandler(metronClient loggingclient.IngressClient, enableContainerProxy bool, proxyMemoryAllocation float64, metricsCache *atomic.Value) *StatsHandler {
	return &StatsHandler{
		cpuInfos:              make(map[string]*cpuInfo),
		repMetricsMap:         make(map[string]*CachedContainerMetrics),
		enableContainerProxy:  enableContainerProxy,
		metronClient:          metronClient,
		proxyMemoryAllocation: proxyMemoryAllocation,
		metricsCache:          metricsCache,
	}
}

func (handler *StatsHandler) Handle(logger lager.Logger, container executor.Container, metric executor.Metrics, timeStamp time.Time) error {
	guid := container.Guid

	previousCPUInfo := handler.cpuInfos[guid]

	if handler.enableContainerProxy && container.EnableContainerProxy {
		metric.MemoryUsageInBytes = uint64(float64(metric.MemoryUsageInBytes) * handler.scaleMemory(container))
		metric.MemoryLimitInBytes = uint64(float64(metric.MemoryLimitInBytes) - handler.proxyMemoryAllocation)
	}

	repMetrics, cpu := handler.calculateAndSendMetrics(logger, metric.MetricsConfig, metric.ContainerMetrics, previousCPUInfo, timeStamp)
	if cpu != nil {
		handler.cpuInfos[guid] = cpu
	}

	if repMetrics != nil {
		handler.repMetricsMap[guid] = repMetrics
	}

	handler.metricsCache.Store(handler.repMetricsMap)
	return nil
}

func (handler *StatsHandler) calculateAndSendMetrics(
	logger lager.Logger,
	metricsConfig executor.MetricsConfig,
	containerMetrics executor.ContainerMetrics,
	previousInfo *cpuInfo,
	now time.Time,
) (*CachedContainerMetrics, *cpuInfo) {
	currentInfo, cpuPercent := calculateInfo(containerMetrics, previousInfo, now)

	if len(metricsConfig.Tags) == 0 {
		metricsConfig.Tags = map[string]string{}
	}

	applicationId := metricsConfig.Guid
	if sourceID, ok := metricsConfig.Tags["source_id"]; ok {
		applicationId = sourceID
	} else {
		metricsConfig.Tags["source_id"] = applicationId
	}

	index := strconv.Itoa(metricsConfig.Index)
	if _, ok := metricsConfig.Tags["instance_id"]; !ok {
		metricsConfig.Tags["instance_id"] = index
	}

	if applicationId != "" {
		err := handler.metronClient.SendAppMetrics(loggingclient.ContainerMetric{
			CpuPercentage:          cpuPercent,
			MemoryBytes:            containerMetrics.MemoryUsageInBytes,
			DiskBytes:              containerMetrics.DiskUsageInBytes,
			MemoryBytesQuota:       containerMetrics.MemoryLimitInBytes,
			DiskBytesQuota:         containerMetrics.DiskLimitInBytes,
			AbsoluteCPUUsage:       uint64(containerMetrics.TimeSpentInCPU.Nanoseconds()),
			AbsoluteCPUEntitlement: containerMetrics.AbsoluteCPUEntitlementInNanoseconds,
			ContainerAge:           containerMetrics.ContainerAgeInNanoseconds,
			Tags:                   metricsConfig.Tags,
		})

		if err != nil {
			logger.Error("failed-to-send-container-metrics", err, lager.Data{
				"metrics_guid":  applicationId,
				"metrics_index": metricsConfig.Index,
				"tags":          metricsConfig.Tags,
			})
		}
	}

	return &CachedContainerMetrics{
		MetricGUID:       applicationId,
		CPUUsageFraction: cpuPercent / 100,
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

func (handler *StatsHandler) scaleMemory(container executor.Container) float64 {
	memFloat := float64(container.MemoryLimit)
	return (memFloat - handler.proxyMemoryAllocation) / memFloat
}
