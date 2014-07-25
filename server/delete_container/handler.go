package delete_container

import (
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

	deleteLog := h.logger.Session("delete-handler")

	err := h.depotClient.DeleteContainer(guid)

	if err != nil {
		deleteLog.Error("failed-to-delete-container", err)
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
}
