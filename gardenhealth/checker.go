package gardenhealth

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/gardenstore"
	"github.com/cloudfoundry-incubator/executor/guidgen"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/garden/server"
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

// NewChecker constructs a checker.
//
// healthcheckSpec describes the process to run in the healthcheck container and
// retryInterval describes the amount of time to wait to sleep when retrying a
// failed garden command.
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

// Healthcheck destroys any existing healthcheck containers, creates a new container,
// runs a process in the new container, waits for the process to exit, then destroys
// the created container.
//
// If any of these steps fail, the failed step will be retried
// up to gardenhealth.MaxRetries times. If the command continues to fail after the
// retries, an error will be returned, indicating the healthcheck failed.
func (c *checker) Healthcheck(logger lager.Logger) (healthcheckResult error) {
	logger = logger.Session("healthcheck")
	logger.Debug("starting")

	logger.Debug("attempting-list")
	var containers []garden.Container
	err := RetryOnFail(c.retryInterval, func() (listErr error) {
		containers, listErr = c.gardenClient.Containers(garden.Properties{
			gardenstore.TagPropertyPrefix + HealthcheckTag: HealthcheckTagValue,
		})
		if listErr != nil {
			logger.Debug("failed-list", lager.Data{"error": listErr})
		}
		return listErr
	})

	if err != nil {
		return err
	}
	logger.Debug("list-succeeded")

	logger.Debug("attempting-initial-destroy")
	for i := range containers {
		err = RetryOnFail(c.retryInterval, func() (destroyErr error) {
			handle := containers[i].Handle()
			destroyErr = c.gardenClient.Destroy(handle)
			if destroyErr != nil {
				if destroyErr.Error() == server.ErrConcurrentDestroy.Error() {
					// Log but don't fail if container is already being destroyed
					logger.Debug("already-being-destroyed-during-initial-destroy", lager.Data{"handle": handle})
					return nil
				}

				logger.Debug("failed-initial-destroy", lager.Data{"error": destroyErr})
			}
			return destroyErr
		})

		if err != nil {
			return err
		}
	}
	logger.Debug("initial-destroy-succeeded")

	logger.Debug("attempting-create")
	guid := HealthcheckPrefix + c.guidGenerator.Guid(logger)
	var container garden.Container
	err = RetryOnFail(c.retryInterval, func() (createErr error) {
		container, createErr = c.gardenClient.Create(garden.ContainerSpec{
			Handle:     guid,
			RootFSPath: c.rootFSPath,
			Properties: garden.Properties{
				gardenstore.ContainerOwnerProperty:             c.containerOwnerName,
				gardenstore.TagPropertyPrefix + HealthcheckTag: HealthcheckTagValue,
			},
		})
		if createErr != nil {
			logger.Debug("failed-create", lager.Data{"error": createErr})
		}
		return createErr
	})

	if err != nil {
		return err
	}
	logger.Debug("create-succeeded")

	defer func() {
		err := RetryOnFail(c.retryInterval, func() (destroyErr error) {
			destroyErr = c.destroyContainer(guid)
			if destroyErr != nil {
				if destroyErr.Error() == server.ErrConcurrentDestroy.Error() {
					// Log but don't fail if container is already being destroyed
					logger.Debug("already-being-destroyed-during-cleanup-destroy", lager.Data{"handle": guid})
					return nil
				}
				logger.Debug("failed-cleanup-destroy", lager.Data{"error": destroyErr})
			}
			return destroyErr
		})
		if err != nil {
			healthcheckResult = err
		}

		logger.Debug("finished")
	}()

	logger.Debug("attempting-run", lager.Data{
		"processPath": c.healthcheckSpec.Path,
		"processArgs": c.healthcheckSpec.Args,
		"processUser": c.healthcheckSpec.User,
		"processEnv":  c.healthcheckSpec.Env,
		"processDir":  c.healthcheckSpec.Dir,
	})

	var proc garden.Process
	err = RetryOnFail(c.retryInterval, func() (runErr error) {
		proc, runErr = container.Run(c.healthcheckSpec, garden.ProcessIO{})
		if runErr != nil {
			logger.Debug("failed-run", lager.Data{"error": runErr})
		}
		return runErr
	})

	if err != nil {
		return err
	}
	logger.Debug("run-succeeded")

	logger.Debug("attempting-wait")
	var exitCode int
	err = RetryOnFail(c.retryInterval, func() (waitErr error) {
		exitCode, waitErr = proc.Wait()
		if waitErr != nil {
			logger.Debug("failed-wait", lager.Data{"error": waitErr})
		}
		return waitErr
	})
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return HealthcheckFailedError(exitCode)
	}

	logger.Debug("wait-succeeded")

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
