package total_resources

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
	rLog := h.logger.Session("total-resources-handler")

	resources, err := h.depotClient.TotalResources()
	if err != nil {
		rLog.Error("failed-to-get-total-resources", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(resources)
	if err != nil {
		rLog.Error("failed-to-marshal-response", err)
		return
	}
}
