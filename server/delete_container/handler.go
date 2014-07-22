package delete_container

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	depotClient api.Client
	logger      *gosteno.Logger
}

func New(depotClient api.Client, logger *gosteno.Logger) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	err := h.depotClient.DeleteContainer(guid)

	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.delete-container.failed")
		switch v := err.(type) {
		case api.Error:
			w.WriteHeader(v.StatusCode())
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	h.logger.Info("executor.delete-container.ok")
	w.WriteHeader(http.StatusOK)
}
