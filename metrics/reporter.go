package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/metric"
)

const (
	totalMemory = metric.Mebibytes("capacity.total.memory")
	totalDisk = metric.Mebibytes("capacity.total.disk")
	totalContainers = metric.Metric("capacity.total.containers")

	remainingMemory = metric.Mebibytes("capacity.remaining.memory")
	remainingDisk = metric.Mebibytes("capacity.remaining.disk")
	remainingContainers = metric.Metric("capacity.remaining.containers")

	containersExpected = metric.Metric("containers.expected")
	containersActual = metric.Metric("containers.actual")
)

type ExecutorSource interface {
	CurrentCapacity() registry.Capacity
	TotalCapacity() registry.Capacity
	GetAllContainers() []api.Container
}

type ActualSource interface {
	Containers() ([]warden.Container, error)
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
