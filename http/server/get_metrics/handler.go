package get_metrics

import (
	"encoding/json"
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
	guid := r.FormValue(":guid")

	getLog := h.logger.Session("get-metrics-handler")

	w.Header().Set("Content-Type", "application/json")

	metrics, err := h.depotClient.GetMetrics(guid)
	if err != nil {
		if err == executor.ErrContainerNotFound {
			getLog.Info("container-not-found")
		} else {
			getLog.Error("failed-to-get-metrics", err)
		}
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(metrics)
	if err != nil {
		getLog.Error("failed-to-marshal-metrics-response", err)
		return
	}
}
