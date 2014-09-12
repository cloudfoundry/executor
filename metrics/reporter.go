package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/dropsonde/autowire/metrics"
)

type Source interface {
	CurrentCapacity() registry.Capacity
	TotalCapacity() registry.Capacity
	GetAllContainers() []api.Container
}

type Reporter struct {
	Source   Source
	Interval time.Duration
}

func (reporter *Reporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)

	for {
		select {
		case <-signals:
			return nil

		case <-time.After(reporter.Interval):
			remainingCapacity := reporter.Source.CurrentCapacity()
			totalCapacity := reporter.Source.TotalCapacity()
			containers := reporter.Source.GetAllContainers()

			metrics.SendValue("capacity.total.memory", float64(totalCapacity.MemoryMB), "Metric")
			metrics.SendValue("capacity.total.disk", float64(totalCapacity.DiskMB), "Metric")
			metrics.SendValue("capacity.total.containers", float64(totalCapacity.Containers), "Metric")

			metrics.SendValue("capacity.remaining.memory", float64(remainingCapacity.MemoryMB), "Metric")
			metrics.SendValue("capacity.remaining.disk", float64(remainingCapacity.DiskMB), "Metric")
			metrics.SendValue("capacity.remaining.containers", float64(remainingCapacity.Containers), "Metric")

			metrics.SendValue("containers", float64(len(containers)), "Metric")
		}
	}
}
