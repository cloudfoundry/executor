package gardenhealth

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/guidgen"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/lager"
)

const (
	HealthcheckPrefix   = "executor-healthcheck-"
	HealthcheckTag      = "healthcheck-tag"
	HealthcheckTagValue = "healthcheck"
)

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
	rootFSPath         string
	containerOwnerName string
	retryInterval      time.Duration
	healthcheckSpec    garden.ProcessSpec
	executorClient     executor.Client
	gardenClient       garden.Client
	guidGenerator      guidgen.Generator
}

func NewChecker(
	rootFSPath string,
	containerOwnerName string,
	retryInterval time.Duration,
	healthcheckSpec garden.ProcessSpec,
	gardenClient garden.Client,
	guidGenerator guidgen.Generator,
) Checker {
	return &checker{
		rootFSPath:         rootFSPath,
		containerOwnerName: containerOwnerName,
		retryInterval:      retryInterval,
		healthcheckSpec:    healthcheckSpec,
		gardenClient:       gardenClient,
		guidGenerator:      guidGenerator,
	}
}

func (c *checker) Healthcheck(logger lager.Logger) (healthcheckResult error) {
	var containers []garden.Container
	err := RetryOnFail(c.retryInterval, func() (listErr error) {
		containers, listErr = c.gardenClient.Containers(garden.Properties{
			HealthcheckTag: HealthcheckTagValue,
		})
		return listErr
	})

	if err != nil {
		return err
	}

	for i := range containers {
		err = RetryOnFail(c.retryInterval, func() (destroyErr error) {
			return c.gardenClient.Destroy(containers[i].Handle())
		})

		if err != nil {
			return err
		}
	}

	guid := HealthcheckPrefix + c.guidGenerator.Guid(logger)

	var container garden.Container
	err = RetryOnFail(c.retryInterval, func() (createErr error) {
		container, createErr = c.gardenClient.Create(garden.ContainerSpec{
			Handle:     guid,
			RootFSPath: c.rootFSPath,
			Properties: garden.Properties{
				gardenstore.ContainerOwnerProperty: c.containerOwnerName,
				HealthcheckTag:                     HealthcheckTagValue,
			},
		})
		return createErr
	})
	if err != nil {
		return err
	}

	defer func() {
		err := RetryOnFail(c.retryInterval, func() error {
			return c.destroyContainer(guid)
		})
		if err != nil {
			healthcheckResult = err
		}
	}()

	var proc garden.Process
	err = RetryOnFail(c.retryInterval, func() (runErr error) {
		proc, runErr = container.Run(c.healthcheckSpec, garden.ProcessIO{})
		return runErr
	})
	if err != nil {
		return err
	}

	var exitCode int
	err = RetryOnFail(c.retryInterval, func() (waitErr error) {
		exitCode, waitErr = proc.Wait()
		return waitErr
	})
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

const (
	MaxRetries = 3
)

func RetryOnFail(retryInterval time.Duration, cmd func() error) error {
	var err error

	for i := 0; i < MaxRetries; i++ {
		err = cmd()
		if err == nil {
			return nil
		}

		time.Sleep(retryInterval)
	}

	return err
}
