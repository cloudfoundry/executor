package executor

import (
	"errors"

	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gosteno"
)

var (
	ContainerNotFound = errors.New("container not found")
	StepsInvalid      = errors.New("steps invalid")
)

type Client interface {
	RunContainer(guid string, actions []models.ExecutorAction, completeURL string) error
}

type client struct {
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
	runActions   chan<- DepotRunAction
	logger       *gosteno.Logger
}

func NewClient(
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	runActions chan<- DepotRunAction,
	logger *gosteno.Logger,
) Client {
	return &client{
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
		runActions:   runActions,
		logger:       logger,
	}
}

func (c *client) RunContainer(guid string, actions []models.ExecutorAction, completeURL string) error {
	registration, err := c.registry.FindByGuid(guid)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.container-not-found")
		return ContainerNotFound
	}

	container, err := c.wardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		c.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.lookup-failed")
		return err
	}

	var result string
	steps, err := c.transformer.StepsFor(registration.Log, actions, container, &result)
	if err != nil {
		c.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.steps-invalid")
		return StepsInvalid
	}

	c.runActions <- DepotRunAction{
		CompleteURL:  completeURL,
		Registration: registration,
		Sequence:     sequence.New(steps),
		Result:       &result,
	}

	return nil
}
