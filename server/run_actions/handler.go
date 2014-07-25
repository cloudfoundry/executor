package run_actions

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
	runLog := h.logger.Session("run-handler")

	var request api.ContainerRunRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		runLog.Error("failed-to-unmarshal-payload", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")

	err = h.depotClient.Run(guid, request)
	if err != nil {
		runLog.Error("run-actions-failed", err)
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
