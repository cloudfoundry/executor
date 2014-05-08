package server

import (
	"net/http"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/server/allocate_container"
	"github.com/cloudfoundry-incubator/executor/server/delete_container"
	"github.com/cloudfoundry-incubator/executor/server/get_container"
	"github.com/cloudfoundry-incubator/executor/server/initialize_container"
	"github.com/cloudfoundry-incubator/executor/server/run_actions"
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
	WaitGroup             *sync.WaitGroup
	Cancel                chan struct{}
}

func New(c *Config) (http.Handler, error) {
	handlers := map[string]http.Handler{
		api.AllocateContainer: allocate_container.New(c.Registry, c.WaitGroup, c.Logger),

		api.GetContainer: get_container.New(c.Registry, c.WaitGroup, c.Logger),

		api.InitializeContainer: initialize_container.New(
			c.ContainerOwnerName,
			c.ContainerMaxCPUShares,
			c.WardenClient,
			c.Registry,
			c.WaitGroup,
			c.Logger,
		),

		api.RunActions: run_actions.New(
			c.WardenClient,
			c.Registry,
			c.Transformer,
			c.WaitGroup,
			c.Cancel,
			c.Logger),

		api.DeleteContainer: delete_container.New(c.WardenClient, c.Registry, c.WaitGroup, c.Logger),
	}

	return router.NewRouter(api.Routes, handlers)
}
