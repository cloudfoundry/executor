package server

import (
	"net/http"
	"os"

	"github.com/cloudfoundry-incubator/executor"
	ehttp "github.com/cloudfoundry-incubator/executor/http"
	"github.com/cloudfoundry-incubator/executor/http/server/allocate_containers"
	"github.com/cloudfoundry-incubator/executor/http/server/delete_container"
	"github.com/cloudfoundry-incubator/executor/http/server/events"
	"github.com/cloudfoundry-incubator/executor/http/server/get_all_metrics"
	"github.com/cloudfoundry-incubator/executor/http/server/get_container"
	"github.com/cloudfoundry-incubator/executor/http/server/get_files"
	"github.com/cloudfoundry-incubator/executor/http/server/get_metrics"
	"github.com/cloudfoundry-incubator/executor/http/server/list_containers"
	"github.com/cloudfoundry-incubator/executor/http/server/ping"
	"github.com/cloudfoundry-incubator/executor/http/server/remaining_resources"
	"github.com/cloudfoundry-incubator/executor/http/server/run_actions"
	"github.com/cloudfoundry-incubator/executor/http/server/stop_container"
	"github.com/cloudfoundry-incubator/executor/http/server/total_resources"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/rata"
)

type Server struct {
	Address             string
	DepotClientProvider executor.ClientProvider
	Logger              lager.Logger
}

type HandlerProvider interface {
	WithLogger(lager.Logger) http.Handler
}

func (s *Server) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	handlers := rata.Handlers{
		ehttp.Ping: ping.New(s.DepotClientProvider.WithLogger(s.Logger)),
	}

	handlerProviders := s.NewHandlerProviders()
	for key, provider := range handlerProviders {
		handlers[key] = LogWrap(provider, s.Logger)
	}

	router, err := rata.NewRouter(ehttp.Routes, handlers)
	if err != nil {
		return err
	}

	server := ifrit.Invoke(http_server.New(s.Address, router))

	close(readyChan)

	for {
		select {
		case sig := <-sigChan:
			server.Signal(sig)
			s.Logger.Info("executor.server.signaled-to-stop")
		case err := <-server.Wait():
			if err != nil {
				s.Logger.Error("server-failed", err)
			}

			s.Logger.Info("executor.server.stopped")
			return err
		}
	}
}

func (s *Server) NewHandlerProviders() map[string]HandlerProvider {
	return map[string]HandlerProvider{
		ehttp.Events:             events.New(s.DepotClientProvider),
		ehttp.AllocateContainers: allocate_containers.New(s.DepotClientProvider),
		ehttp.GetContainer:       get_container.New(s.DepotClientProvider),
		ehttp.ListContainers:     list_containers.New(s.DepotClientProvider),
		ehttp.RunContainer:       run_actions.New(s.DepotClientProvider),
		ehttp.StopContainer:      stop_container.New(s.DepotClientProvider),
		ehttp.DeleteContainer:    delete_container.New(s.DepotClientProvider),

		ehttp.GetTotalResources:     total_resources.New(s.DepotClientProvider),
		ehttp.GetRemainingResources: remaining_resources.New(s.DepotClientProvider),

		ehttp.GetFiles:   get_files.New(s.DepotClientProvider),
		ehttp.GetMetrics: get_metrics.New(s.DepotClientProvider),

		ehttp.GetAllMetrics: get_all_metrics.New(s.DepotClientProvider),
	}
}
