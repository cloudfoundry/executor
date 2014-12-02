package run_actions

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/http/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type Generator struct {
	depotClientProvider executor.ClientProvider
}

type handler struct {
	depotClient executor.Client
	logger      lager.Logger
}

func New(depotClientProvider executor.ClientProvider) *Generator {
	return &Generator{
		depotClientProvider: depotClientProvider,
	}
}

func (generator *Generator) WithLogger(logger lager.Logger) http.Handler {
	return &handler{
		depotClient: generator.depotClientProvider.WithLogger(logger),
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	runLog := h.logger.Session("run-handler")

	guid := r.FormValue(":guid")

	err := h.depotClient.RunContainer(guid)
	if err != nil {
		runLog.Error("run-actions-failed", err)
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
