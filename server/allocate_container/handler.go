package allocate_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type Handler struct {
	depotClient api.Client
	logger      lager.Logger
}

func New(depotClient api.Client, logger lager.Logger) *Handler {
	return &Handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allocLog := h.logger.Session("allocate-handler")

	req := api.ContainerAllocationRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		allocLog.Error("failed-to-unmarshal-payload", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")

	container, err := h.depotClient.AllocateContainer(guid, req)
	if err != nil {
		allocLog.Error("failed-to-allocate-container", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(container)
	if err != nil {
		allocLog.Error("failed-to-marshal-response", err)
		return
	}
}
