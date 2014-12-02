package allocate_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/http/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type Provider struct {
	depotClientProvider executor.ClientProvider
}

type handler struct {
	depotClient executor.Client
	logger      lager.Logger
}

func New(depotClientProvider executor.ClientProvider) *Provider {
	return &Provider{
		depotClientProvider: depotClientProvider,
	}
}

func (provider *Provider) WithLogger(logger lager.Logger) http.Handler {
	return &handler{
		depotClient: provider.depotClientProvider.WithLogger(logger),
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allocLog := h.logger.Session("allocate-handler")

	req := executor.Container{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		allocLog.Error("failed-to-unmarshal-payload", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	container, err := h.depotClient.AllocateContainer(req)
	if err != nil {
		allocLog.Error("failed-to-allocate-container", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(container)
	if err != nil {
		allocLog.Error("failed-to-marshal-response", err)
		return
	}
}
