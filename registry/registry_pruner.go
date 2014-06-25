package registry

import (
	"os"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry/gunk/timeprovider"
)

type RegistryPruner struct {
	registry     Registry
	timeProvider timeprovider.TimeProvider
	interval     time.Duration
}

func NewPruner(registry Registry, timeProvider timeprovider.TimeProvider, interval time.Duration) *RegistryPruner {
	return &RegistryPruner{
		registry:     registry,
		timeProvider: timeProvider,
		interval:     interval,
	}
}

func (p *RegistryPruner) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	ticker := p.timeProvider.NewTickerChannel("pruner", p.interval)
	close(readyChan)

	for {
		select {
		case <-ticker:
			p.prune()
		case sig := <-sigChan:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				return nil
			}
		}
	}
}

func (p *RegistryPruner) prune() {
	for _, container := range p.registry.GetAllContainers() {
		if container.State != api.StateReserved {
			continue
		}
		if p.timeSinceContainerAllocated(container) >= p.interval {
			p.registry.Delete(container.Guid)
		}
	}
}

func (p *RegistryPruner) timeSinceContainerAllocated(container api.Container) time.Duration {
	return p.timeProvider.Time().Sub(time.Unix(0, container.AllocatedAt))
}
