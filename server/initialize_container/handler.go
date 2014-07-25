package initialize_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type handler struct {
	depotClient api.Client
	logger      lager.Logger
}

func New(depotClient api.Client, logger lager.Logger) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	initLog := h.logger.Session("initialize-handler")

	var req api.ContainerInitializationRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		initLog.Error("failed-to-unmarshal-payload", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")
	reg, err := h.depotClient.InitializeContainer(guid, req)
	if err != nil {
		initLog.Error("failed-to-initialize-container", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(reg)
	if err != nil {
		initLog.Error("failed-to-marshal-response", err)
		return
	}
}
