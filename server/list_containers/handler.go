package list_containers

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
	resources := h.registry.GetAllContainers()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(resources)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.list-containers.writing-body-failed")
		return
	}

	h.logger.Info("executor.list-containeres.ok")
}
