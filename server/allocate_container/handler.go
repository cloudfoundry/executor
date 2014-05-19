package allocate_container

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry/gosteno"
)

type Handler struct {
	registry  registry.Registry
	waitGroup *sync.WaitGroup
	logger    *gosteno.Logger
}

func New(registry registry.Registry, waitGroup *sync.WaitGroup, logger *gosteno.Logger) *Handler {
	return &Handler{
		registry:  registry,
		waitGroup: waitGroup,
		logger:    logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.waitGroup.Add(1)
	defer h.waitGroup.Done()

	req := api.ContainerAllocationRequest{}
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

	guid := r.FormValue(":guid")

	container, err := h.registry.Reserve(guid, req)
	if err == registry.ErrContainerAlreadyExists {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
			"guid":  guid,
		}, "executor.allocate-container.container-already-exists")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
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
