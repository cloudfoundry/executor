package list_containers

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/registry"
)

type handler struct {
	registry registry.Registry
}

func New(registry registry.Registry) http.Handler {
	return &handler{
		registry: registry,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resources := h.registry.GetAllContainers()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resources)
}
