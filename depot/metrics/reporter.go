package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
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
	RemainingResources() (executor.ExecutorResources, error)
	TotalResources() (executor.ExecutorResources, error)
	ListContainers() ([]executor.Container, error)
}

type Reporter struct {
	Interval       time.Duration
	ExecutorSource ExecutorSource
	Logger         lager.Logger
}

func (reporter *Reporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)

	for {
		select {
		case <-signals:
			return nil

		case <-time.After(reporter.Interval):
			remainingCapacity, err := reporter.ExecutorSource.RemainingResources()
			if err != nil {
				reporter.Logger.Error("failed-remaining-resources", err)
				remainingCapacity.Containers = -1
				remainingCapacity.DiskMB = -1
				remainingCapacity.MemoryMB = -1
			}

			totalCapacity, err := reporter.ExecutorSource.TotalResources()
			if err != nil {
				reporter.Logger.Error("failed-total-resources", err)
				totalCapacity.Containers = -1
				totalCapacity.DiskMB = -1
				totalCapacity.MemoryMB = -1
			}

			var nContainers int
			containers, err := reporter.ExecutorSource.ListContainers()
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
		}
	}
}
