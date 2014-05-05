package allocate_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api/containers"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gosteno"
)

type Handler struct {
	registry registry.Registry
	logger   *gosteno.Logger
}

func New(registry registry.Registry, logger *gosteno.Logger) *Handler {
	return &Handler{
		registry: registry,
		logger:   logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := containers.ContainerAllocationRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.bad-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.CpuPercent > 100 || req.CpuPercent < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	container, err := h.registry.Reserve(req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.full")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(container)
}
