package api

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api/allocate_container"
	"github.com/cloudfoundry-incubator/executor/api/get_container"
	"github.com/cloudfoundry-incubator/executor/api/initialize_container"
	"github.com/cloudfoundry-incubator/executor/api/run_actions"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/routes"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/tedsuo/router"
)

type Config struct {
	Registry registry.Registry

	WardenClient          warden.Client
	ContainerOwnerName    string
	ContainerMaxCPUShares uint64

	Transformer *transformer.Transformer
}

func New(c *Config) (http.Handler, error) {
	handlers := map[string]http.Handler{
		routes.AllocateContainer: allocate_container.New(c.Registry),
		routes.GetContainer:      get_container.New(c.Registry),

		routes.InitializeContainer: initialize_container.New(
			c.ContainerOwnerName,
			c.ContainerMaxCPUShares,
			c.WardenClient,
			c.Registry,
		),

		routes.RunActions: run_actions.New(c.WardenClient, c.Registry, c.Transformer),

		routes.DeleteContainer: notImplemented(),
	}

	return router.NewRouter(routes.Routes, handlers)
}

func notImplemented() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no.")
	})
}
