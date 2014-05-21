package remaining_resources

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

	currentCapacity := h.registry.CurrentCapacity()

	resources := api.ExecutorResources{
		MemoryMB:   currentCapacity.MemoryMB,
		DiskMB:     currentCapacity.DiskMB,
		Containers: currentCapacity.Containers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resources)
}
