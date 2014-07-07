package allocate_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
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
	req := api.ContainerAllocationRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.bad-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")

	container, err := h.registry.Reserve(guid, req)
	if err == registry.ErrContainerAlreadyExists {
		h.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
			"guid":  guid,
		}, "executor.allocate-container.container-already-exists")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.full")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(container)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.writing-body-failed")
		return
	}

	h.logger.Info("executor.allocate-container.ok")
}
