package list_containers

import (
	"encoding/json"
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
	resources, err := h.depotClient.ListContainers()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err = json.NewEncoder(w).Encode(resources)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.list-containers.writing-body-failed")
		return
	}

	h.logger.Info("executor.list-containeres.ok")
}
