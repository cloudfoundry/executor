package api

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api/allocate_container"
	"github.com/cloudfoundry-incubator/executor/api/delete_container"
	"github.com/cloudfoundry-incubator/executor/api/get_container"
	"github.com/cloudfoundry-incubator/executor/api/initialize_container"
	"github.com/cloudfoundry-incubator/executor/api/run_actions"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/routes"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
	"github.com/tedsuo/router"
)

type Config struct {
	Registry              registry.Registry
	WardenClient          warden.Client
	ContainerOwnerName    string
	ContainerMaxCPUShares uint64
	Transformer           *transformer.Transformer
	Logger                *gosteno.Logger
}

func New(c *Config) (http.Handler, error) {
	handlers := map[string]http.Handler{
		routes.AllocateContainer: allocate_container.New(c.Registry, c.Logger),
		routes.GetContainer:      get_container.New(c.Registry, c.Logger),

		routes.InitializeContainer: initialize_container.New(
			c.ContainerOwnerName,
			c.ContainerMaxCPUShares,
			c.WardenClient,
			c.Registry,
			c.Logger,
		),

		routes.RunActions: run_actions.New(c.WardenClient, c.Registry, c.Transformer, c.Logger),

		routes.DeleteContainer: delete_container.New(c.WardenClient, c.Registry, c.Logger),
	}

	return router.NewRouter(routes.Routes, handlers)
}
