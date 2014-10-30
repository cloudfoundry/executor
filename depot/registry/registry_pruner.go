package registry

import (
	"os"
	"time"

	garden "github.com/cloudfoundry-incubator/garden/api"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/pivotal-golang/lager"
)

type RegistryPruner struct {
	registry         Registry
	exchanger        exchanger.Exchanger
	allocationClient garden.Client
	timeProvider     timeprovider.TimeProvider
	interval         time.Duration
	logger           lager.Logger
}

func NewPruner(registry Registry, allocationClient garden.Client, exchanger exchanger.Exchanger, timeProvider timeprovider.TimeProvider, interval time.Duration, logger lager.Logger) *RegistryPruner {
	return &RegistryPruner{
		registry:         registry,
		allocationClient: allocationClient,
		timeProvider:     timeProvider,
		interval:         interval,
		exchanger:        exchanger,
		logger:           logger.Session("registry-pruner"),
	}
}

func (p *RegistryPruner) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	ticker := p.timeProvider.NewTickerChannel("pruner", p.interval)
	close(readyChan)
	p.logger.Info("started")

	for {
		select {
		case <-ticker:
			p.prune()
		case <-sigChan:
			p.logger.Info("stopped")
			return nil
		}
	}
}

func (p *RegistryPruner) prune() {
	pLog := p.logger.Session("prune")

	containers, err := p.allocationClient.Containers(garden.Properties{
		exchanger.ContainerStateProperty: string(executor.StateReserved),
	})

	if err != nil {
		p.logger.Error("failed-to-read-containers", err)
	}

	for _, gardenContainer := range containers {
		container, err := p.exchanger.Garden2Executor(gardenContainer)
		if err != nil {
			p.logger.Error("failed-to-convert-container", err)
		}

		lifespan := p.timeSinceContainerAllocated(container)

		if lifespan >= p.interval {
			pLog := pLog.Session("prune", lager.Data{
				"container-guid": container.Guid,
				"lifespan":       lifespan.String(),
			})

			pLog.Debug("pruning-reserved-container")

			err := p.allocationClient.Destroy(gardenContainer.Handle())
			if err != nil {
				pLog.Error("failed-to-destroy-stale-allocation", err)
				continue
			}

			p.registry.Delete(container)
		}
	}

	pLog.Info("done")
}

func (p *RegistryPruner) timeSinceContainerAllocated(container executor.Container) time.Duration {
	return p.timeProvider.Time().Sub(time.Unix(0, container.AllocatedAt))
}
