package registry

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/pivotal-golang/lager"
)

type RegistryPruner struct {
	registry     Registry
	timeProvider timeprovider.TimeProvider
	interval     time.Duration
	logger       lager.Logger
}

func NewPruner(registry Registry, timeProvider timeprovider.TimeProvider, interval time.Duration, logger lager.Logger) *RegistryPruner {
	return &RegistryPruner{
		registry:     registry,
		timeProvider: timeProvider,
		interval:     interval,
		logger:       logger.Session("registry-pruner"),
	}
}

func (p *RegistryPruner) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	ticker := p.timeProvider.NewTickerChannel("pruner", p.interval)
	close(readyChan)

	for {
		select {
		case <-ticker:
			p.prune()
		case <-sigChan:
			return nil
		}
	}
}

func (p *RegistryPruner) prune() {
	for _, container := range p.registry.GetAllContainers() {
		if container.State != api.StateReserved {
			continue
		}
		lifespan := p.timeSinceContainerAllocated(container)
		if lifespan >= p.interval {
			p.logger.Info("pruning-reserved-container", lager.Data{"container-guid": container.Guid, "container-lifespan": lifespan.String()})
			err := p.registry.Delete(container.Guid)
			if err != nil {
				p.logger.Error("pruning-reserved-container-failed", err, lager.Data{"container-guid": container.Guid, "container-lifespan": lifespan.String()})
			}
		}
	}
}

func (p *RegistryPruner) timeSinceContainerAllocated(container api.Container) time.Duration {
	return p.timeProvider.Time().Sub(time.Unix(0, container.AllocatedAt))
}
