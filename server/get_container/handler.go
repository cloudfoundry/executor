package get_container

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
	guid := r.FormValue(":guid")

	getLog := h.logger.Session("get-handler")

	resource, err := h.depotClient.GetContainer(guid)
	if err != nil {
		getLog.Error("failed-to-get-container", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(resource)
	if err != nil {
		getLog.Error("failed-to-marshal-response", err)
		return
	}
}
