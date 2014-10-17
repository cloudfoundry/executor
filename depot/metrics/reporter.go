package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/registry"
	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
)

const (
	totalMemory     = metric.Mebibytes("CapacityTotalMemory")
	totalDisk       = metric.Mebibytes("CapacityTotalDisk")
	totalContainers = metric.Metric("CapacityTotalContainers")

	remainingMemory     = metric.Mebibytes("CapacityRemainingMemory")
	remainingDisk       = metric.Mebibytes("CapacityRemainingDisk")
	remainingContainers = metric.Metric("CapacityRemainingContainers")

	containersExpected = metric.Metric("ContainersExpected")
	containersActual   = metric.Metric("ContainersActual")
)

type ExecutorSource interface {
	CurrentCapacity() registry.Capacity
	TotalCapacity() registry.Capacity
	GetAllContainers() []executor.Container
}

type ActualSource interface {
	Containers() ([]garden_api.Container, error)
}

type Reporter struct {
	Interval time.Duration

	ExecutorSource ExecutorSource
	ActualSource   ActualSource
}

func (reporter *Reporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)

	for {
		select {
		case <-signals:
			return nil

		case <-time.After(reporter.Interval):
			remainingCapacity := reporter.ExecutorSource.CurrentCapacity()
			totalCapacity := reporter.ExecutorSource.TotalCapacity()
			containers := reporter.ExecutorSource.GetAllContainers()

			gardenContainers := -1

			actualContainers, err := reporter.ActualSource.Containers()
			if err == nil {
				gardenContainers = len(actualContainers)
			}

			totalMemory.Send(totalCapacity.MemoryMB)
			totalDisk.Send(totalCapacity.DiskMB)
			totalContainers.Send(totalCapacity.Containers)

			remainingMemory.Send(remainingCapacity.MemoryMB)
			remainingDisk.Send(remainingCapacity.DiskMB)
			remainingContainers.Send(remainingCapacity.Containers)

			containersExpected.Send(len(containers))
			containersActual.Send(gardenContainers)
		}
	}
}
