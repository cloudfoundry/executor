package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

const (
	totalMemory     = metric.Mebibytes("CapacityTotalMemory")
	totalDisk       = metric.Mebibytes("CapacityTotalDisk")
	totalContainers = metric.Metric("CapacityTotalContainers")

	remainingMemory     = metric.Mebibytes("CapacityRemainingMemory")
	remainingDisk       = metric.Mebibytes("CapacityRemainingDisk")
	remainingContainers = metric.Metric("CapacityRemainingContainers")

	containerCount = metric.Metric("ContainerCount")
)

type ExecutorSource interface {
	RemainingResources(lager.Logger) (executor.ExecutorResources, error)
	TotalResources(lager.Logger) (executor.ExecutorResources, error)
	ListContainers(lager.Logger) ([]executor.Container, error)
}

type Reporter struct {
	Interval       time.Duration
	ExecutorSource ExecutorSource
	Clock          clock.Clock
	Logger         lager.Logger
}

func (reporter *Reporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := reporter.Logger.Session("metrics-reporter")

	close(ready)

	timer := reporter.Clock.NewTimer(reporter.Interval)

	for {
		select {
		case <-signals:
			return nil

		case <-timer.C():
			remainingCapacity, err := reporter.ExecutorSource.RemainingResources(logger)
			if err != nil {
				reporter.Logger.Error("failed-remaining-resources", err)
				remainingCapacity.Containers = -1
				remainingCapacity.DiskMB = -1
				remainingCapacity.MemoryMB = -1
			}

			totalCapacity, err := reporter.ExecutorSource.TotalResources(logger)
			if err != nil {
				reporter.Logger.Error("failed-total-resources", err)
				totalCapacity.Containers = -1
				totalCapacity.DiskMB = -1
				totalCapacity.MemoryMB = -1
			}

			var nContainers int
			containers, err := reporter.ExecutorSource.ListContainers(logger)
			if err != nil {
				reporter.Logger.Error("failed-to-list-containers", err)
				nContainers = -1
			} else {
				nContainers = len(containers)
			}

			totalMemory.Send(totalCapacity.MemoryMB)
			totalDisk.Send(totalCapacity.DiskMB)
			totalContainers.Send(totalCapacity.Containers)

			remainingMemory.Send(remainingCapacity.MemoryMB)
			remainingDisk.Send(remainingCapacity.DiskMB)
			remainingContainers.Send(remainingCapacity.Containers)

			containerCount.Send(nContainers)
			timer.Reset(reporter.Interval)
		}
	}
}
