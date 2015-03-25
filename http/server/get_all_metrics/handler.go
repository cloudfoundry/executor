package get_all_metrics

import (
	"encoding/json"
	"net/http"
	"strings"

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
	getLog := h.logger.Session("get-all-metrics-handler")

	tags := executor.Tags{}

	for _, tag := range r.URL.Query()["tag"] {
		segments := strings.SplitN(tag, ":", 2)
		if len(segments) == 2 {
			tags[segments[0]] = segments[1]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	metrics, err := h.depotClient.GetAllMetrics(tags)
	if err != nil {
		getLog.Error("failed-to-get-metrics", err)
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(metrics)
	if err != nil {
		getLog.Error("failed-to-marshal-metrics-response", err)
		error_headers.Write(err, w)
		return
	}
}
