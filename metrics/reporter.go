package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/dropsonde/autowire/metrics"
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

const megabyte = 1024 * 1024

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

			metrics.SendValue("capacity.total.memory", float64(totalCapacity.MemoryMB*megabyte), "B")
			metrics.SendValue("capacity.total.disk", float64(totalCapacity.DiskMB*megabyte), "B")
			metrics.SendValue("capacity.total.containers", float64(totalCapacity.Containers), "Metric")

			metrics.SendValue("capacity.remaining.memory", float64(remainingCapacity.MemoryMB*megabyte), "B")
			metrics.SendValue("capacity.remaining.disk", float64(remainingCapacity.DiskMB*megabyte), "B")
			metrics.SendValue("capacity.remaining.containers", float64(remainingCapacity.Containers), "Metric")

			metrics.SendValue("containers.expected", float64(len(containers)), "Metric")

			gardenContainers := -1

			actualContainers, err := reporter.ActualSource.Containers()
			if err == nil {
				gardenContainers = len(actualContainers)
			}

			metrics.SendValue("containers.actual", float64(gardenContainers), "Metric")
		}
	}
}
