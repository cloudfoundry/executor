package get_container

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
	guid := r.FormValue(":guid")

	getLog := h.logger.Session("get-handler")

	resource, err := h.depotClient.GetContainer(guid)
	if err != nil {
		if err == executor.ErrContainerNotFound {
			getLog.Info("failed-to-get-container")
		} else {
			getLog.Error("failed-to-get-container", err)
		}
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(resource)
	if err != nil {
		getLog.Error("failed-to-marshal-response", err)
		return
	}
}
