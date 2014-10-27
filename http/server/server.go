package server

import (
	"os"

	"github.com/cloudfoundry-incubator/executor"
	ehttp "github.com/cloudfoundry-incubator/executor/http"
	"github.com/cloudfoundry-incubator/executor/http/server/allocate_container"
	"github.com/cloudfoundry-incubator/executor/http/server/delete_container"
	"github.com/cloudfoundry-incubator/executor/http/server/get_container"
	"github.com/cloudfoundry-incubator/executor/http/server/get_files"
	"github.com/cloudfoundry-incubator/executor/http/server/list_containers"
	"github.com/cloudfoundry-incubator/executor/http/server/ping"
	"github.com/cloudfoundry-incubator/executor/http/server/remaining_resources"
	"github.com/cloudfoundry-incubator/executor/http/server/run_actions"
	"github.com/cloudfoundry-incubator/executor/http/server/total_resources"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/rata"
)

type Server struct {
	Address     string
	DepotClient executor.Client
	Logger      lager.Logger
}

func (s *Server) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	handlers := s.NewHandlers()
	for key, handler := range handlers {
		if key != ehttp.Ping {
			handlers[key] = LogWrap(handler, s.Logger)
		}
	}

	router, err := rata.NewRouter(ehttp.Routes, handlers)
	if err != nil {
		return err
	}

	server := ifrit.Envoke(http_server.New(s.Address, router))

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

func (s *Server) NewHandlers() rata.Handlers {
	return rata.Handlers{
		ehttp.AllocateContainer:     allocate_container.New(s.DepotClient, s.Logger),
		ehttp.GetContainer:          get_container.New(s.DepotClient, s.Logger),
		ehttp.ListContainers:        list_containers.New(s.DepotClient, s.Logger),
		ehttp.DeleteContainer:       delete_container.New(s.DepotClient, s.Logger),
		ehttp.GetRemainingResources: remaining_resources.New(s.DepotClient, s.Logger),
		ehttp.GetTotalResources:     total_resources.New(s.DepotClient, s.Logger),
		ehttp.Ping:                  ping.New(s.DepotClient),
		ehttp.RunContainer:          run_actions.New(s.DepotClient, s.Logger),
		ehttp.GetFiles:              get_files.New(s.DepotClient, s.Logger),
	}
}
