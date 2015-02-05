package containermetrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type StatsReporter struct {
	logger lager.Logger

	interval       time.Duration
	clock          clock.Clock
	executorClient executor.Client

	cpuInfos map[string]cpuInfo
}

type cpuInfo struct {
	timeSpentInCPU time.Duration
	timeOfSample   time.Time
}

func NewStatsReporter(logger lager.Logger, interval time.Duration, clock clock.Clock, executorClient executor.Client) *StatsReporter {
	return &StatsReporter{
		logger: logger,

		interval:       interval,
		clock:          clock,
		executorClient: executorClient,

		cpuInfos: make(map[string]cpuInfo),
	}
}

func (reporter *StatsReporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)

	ticker := reporter.clock.NewTicker(reporter.interval)

	for {
		select {
		case <-signals:
			return nil

		case <-ticker.C():
			reporter.emitContainerMetrics(reporter.logger.Session("tick"))
		}
	}

	return nil
}

func (reporter *StatsReporter) emitContainerMetrics(logger lager.Logger) {
	startTime := reporter.clock.Now()

	logger.Info("started")
	defer func() {
		logger.Info("done", lager.Data{"took": reporter.clock.Now().Sub(startTime)})
	}()

	containers, err := reporter.executorClient.ListContainers(nil)
	if err != nil {
		logger.Error("failed-to-list-containers", err)
		return
	}

	logger.Info("emitting", lager.Data{
		"total-containers":        len(containers),
		"listing-containers-took": reporter.clock.Now().Sub(startTime),
	})

	for _, container := range containers {
		if container.MetricsConfig.Guid == "" {
			continue
		}

		currentInfo := cpuInfo{
			timeSpentInCPU: container.TimeSpentInCPU,
			timeOfSample:   reporter.clock.Now(),
		}

		previousInfo, found := reporter.cpuInfos[container.Guid]

		reporter.cpuInfos[container.Guid] = currentInfo

		var cpuPercent float64
		if !found {
			cpuPercent = 0.0
		} else {
			cpuPercent = computeCPUPercent(
				previousInfo.timeSpentInCPU,
				currentInfo.timeSpentInCPU,
				previousInfo.timeOfSample,
				currentInfo.timeOfSample,
			)
		}

		var index int32
		if container.MetricsConfig.Index != nil {
			index = int32(*container.MetricsConfig.Index)
		} else {
			index = -1
		}

		err = metrics.SendContainerMetric(container.MetricsConfig.Guid, index, cpuPercent, container.MemoryUsageInBytes, container.DiskUsageInBytes)
		if err != nil {
			logger.Error("failed-to-send-container-metrics", err)
		}
	}
}

// scale from 0 - 100
func computeCPUPercent(timeSpentA, timeSpentB time.Duration, sampleTimeA, sampleTimeB time.Time) float64 {
	// divide change in time spent in CPU over time between samples.
	// result is out of 100.
	//
	// don't worry about overflowing int64. it's like, 30 years.
	return float64((timeSpentB-timeSpentA)*100) / float64(sampleTimeB.UnixNano()-sampleTimeA.UnixNano())
}
