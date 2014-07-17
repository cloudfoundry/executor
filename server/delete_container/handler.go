package delete_container

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor/executor"

	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	depotClient executor.Client
	logger      *gosteno.Logger
}

func New(depotClient executor.Client, logger *gosteno.Logger) http.Handler {
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
		switch err {
		case executor.ContainerNotFound:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	h.logger.Info("executor.delete-container.ok")
	w.WriteHeader(http.StatusOK)
}
