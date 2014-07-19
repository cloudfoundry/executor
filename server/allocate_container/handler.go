package allocate_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/depot"
	"github.com/cloudfoundry/gosteno"
)

type Handler struct {
	depotClient depot.Client
	logger      *gosteno.Logger
}

func New(depotClient depot.Client, logger *gosteno.Logger) *Handler {
	return &Handler{
		depotClient: depotClient,
		logger:      logger,
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

	container, err := h.depotClient.AllocateContainer(guid, req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.allocate-container.failed")
		switch err {
		case depot.ContainerGuidNotAvailable:
			w.WriteHeader(http.StatusBadRequest)
		case depot.InsufficientResourcesAvailable:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
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
