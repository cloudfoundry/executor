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
	pLog := p.logger.Session("prune")

	for _, container := range p.registry.GetAllContainers() {
		if container.State != api.StateReserved {
			continue
		}

		lifespan := p.timeSinceContainerAllocated(container)

		if lifespan >= p.interval {
			pLog := pLog.Session("prune", lager.Data{
				"container-guid": container.Guid,
				"lifespan":       lifespan.String(),
			})

			pLog.Debug("pruning-reserved-container")

			container, err := p.registry.MarkForDelete(container.Guid)
			if err != nil {
				pLog.Debug("failed-to-mark-for-deleting", lager.Data{
					"error": err.Error(),
				})

				return
			}

			err = p.registry.Delete(container.Guid)
			if err != nil {
				pLog.Error("failed-to-delete-container", err)
				return
			}

			pLog.Info("done")
		}
	}
}

func (p *RegistryPruner) timeSinceContainerAllocated(container api.Container) time.Duration {
	return p.timeProvider.Time().Sub(time.Unix(0, container.AllocatedAt))
}
