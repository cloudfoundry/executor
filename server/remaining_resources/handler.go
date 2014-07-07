package remaining_resources

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
	currentCapacity := h.registry.CurrentCapacity()

	resources := api.ExecutorResources{
		MemoryMB:   currentCapacity.MemoryMB,
		DiskMB:     currentCapacity.DiskMB,
		Containers: currentCapacity.Containers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(resources)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.remaining-resources.writing-body-failed")
		return
	}
	h.logger.Info("executor.remaining-resources.ok")
}
