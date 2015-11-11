package healthstate

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

const HealthcheckPrefix = "executor-healthcheck"

type GardenChecker struct {
	rootfsPath        string
	executorClient    executor.Client
	gardenStoreClient depot.GardenStore
	interval          time.Duration
	clock             clock.Clock
	logger            lager.Logger
}

func New(rootfsPath string, executorClient executor.Client, gardenStoreClient depot.GardenStore, clock clock.Clock, interval time.Duration, logger lager.Logger) *GardenChecker {
	return &GardenChecker{
		rootfsPath:        rootfsPath,
		executorClient:    executorClient,
		gardenStoreClient: gardenStoreClient,
		clock:             clock,
		interval:          interval,
		logger:            logger.Session("garden-health-check"),
	}
}

func (g *GardenChecker) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)

	g.logger.Info("starting")
	ticker := g.clock.NewTicker(g.interval)
	defer ticker.Stop()

	failures := 0
	healthy := true
	for {
		select {
		case <-signals:
			g.logger.Info("complete")
			return nil

		case <-ticker.C():
			g.logger.Info("check-starting")

			passed := g.CheckHealth()
			if passed {
				g.logger.Info("passed-health-check")
				failures = 0
			} else {
				g.logger.Info("failed-health-check")
				failures++
			}

			if failures >= 3 {
				g.logger.Error("set-state-unhealthy", nil)
				g.executorClient.SetHealthy(false)
				healthy = false
			} else if !healthy {
				g.logger.Info("set-state-healthy")
				g.executorClient.SetHealthy(true)
				healthy = true
			}

			g.logger.Info("check-complete")
		}
	}

	return nil
}

func (g *GardenChecker) CheckHealth() bool {
	guid, err := uuid.NewV4()
	if err != nil {
		g.logger.Fatal("failed-to-generate-guid", err)
	}

	newContainer := executor.Container{
		Guid:  HealthcheckPrefix + guid.String(),
		State: executor.StateInitializing,
		Resource: executor.Resource{
			RootFSPath: g.rootfsPath,
		},
		Tags: executor.Tags{
			executor.HealthcheckTag: executor.HealthcheckTagValue,
		},
	}

	g.logger.Info("creating-garden-test-container")
	_, err = g.gardenStoreClient.Create(g.logger, newContainer)
	if err != nil {
		g.logger.Error("failed-creating-garden-test-container", err)
		return false
	}

	g.logger.Info("destroying-garden-test-container")
	err = g.gardenStoreClient.Destroy(g.logger, newContainer.Guid)
	if err != nil {
		g.logger.Error("failed-destroying-garden-test-container", err)
		return false
	}

	return true
}
