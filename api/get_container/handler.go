package get_container

import (
	"encoding/json"
	"log"
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
	guid := r.FormValue(":guid")

	resource, err := h.registry.FindByGuid(guid)
	if err != nil {
		log.Println("no", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resource)
}
