package list_containers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cloudfoundry-incubator/executor/registry"
)

type handler struct {
	registry  registry.Registry
	waitGroup *sync.WaitGroup
}

func New(registry registry.Registry, waitGroup *sync.WaitGroup) http.Handler {
	return &handler{
		registry:  registry,
		waitGroup: waitGroup,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.waitGroup.Add(1)
	defer h.waitGroup.Done()

	resources := h.registry.GetAllContainers()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resources)
}
