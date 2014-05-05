package get_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	registry registry.Registry
	logger   *gosteno.Logger
}

func New(registry registry.Registry, logger *gosteno.Logger) http.Handler {
	return &handler{
		registry: registry,
		logger:   logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	resource, err := h.registry.FindByGuid(guid)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.get-container.not-found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resource)
}
