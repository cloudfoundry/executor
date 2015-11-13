package gardenhealth

import (
	"fmt"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/guidgen"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/lager"
)

const HealthcheckPrefix = "executor-healthcheck-"

type UnrecoverableError string

func (e UnrecoverableError) Error() string {
	return string(e)
}

type HealthcheckFailedError int

func (e HealthcheckFailedError) Error() string {
	return fmt.Sprintf("Healthcheck exited with %d", e)
}

//go:generate counterfeiter -o fakegardenhealth/fake_checker.go . Checker

type Checker interface {
	Healthcheck(lager.Logger) error
}

type checker struct {
	rootfsPath         string
	containerOwnerName string
	healthcheckSpec    garden.ProcessSpec
	executorClient     executor.Client
	gardenClient       garden.Client
	guidGenerator      guidgen.Generator
}

func NewChecker(
	rootfsPath string,
	containerOwnerName string,
	healthcheckSpec garden.ProcessSpec,
	gardenClient garden.Client,
	guidGenerator guidgen.Generator,
) Checker {
	return &checker{
		rootfsPath:         rootfsPath,
		containerOwnerName: containerOwnerName,
		healthcheckSpec:    healthcheckSpec,
		gardenClient:       gardenClient,
		guidGenerator:      guidGenerator,
	}
}

func (c *checker) Healthcheck(logger lager.Logger) (healthcheckResult error) {
	guid := HealthcheckPrefix + c.guidGenerator.Guid(logger)

	container, err := c.gardenClient.Create(garden.ContainerSpec{
		Handle:     guid,
		RootFSPath: c.rootfsPath,
		Properties: garden.Properties{
			gardenstore.ContainerOwnerProperty: c.containerOwnerName,
		},
	})

	if err != nil {
		return err
	}

	defer func() {
		err := c.destroyContainer(guid)
		if err != nil {
			healthcheckResult = err
		}
	}()

	proc, err := container.Run(c.healthcheckSpec, garden.ProcessIO{})

	if err != nil {
		return err
	}

	exitCode, err := proc.Wait()
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return HealthcheckFailedError(exitCode)
	}

	return nil
}

func (c *checker) destroyContainer(guid string) error {
	err := c.gardenClient.Destroy(guid)
	switch err.(type) {
	case nil:
		return nil
	case garden.ContainerNotFoundError:
		return err
	default:
		return UnrecoverableError(err.Error())
	}
}
