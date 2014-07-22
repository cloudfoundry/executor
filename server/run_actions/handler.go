package run_actions

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"

	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	depotClient api.Client
	logger      *gosteno.Logger
}

func New(
	depotClient api.Client,
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request api.ContainerRunRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.invalid-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")

	err = h.depotClient.Run(guid, request)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.failed")
		switch err {
		case api.ErrContainerNotFound:
			w.WriteHeader(http.StatusNotFound)
		case api.ErrStepsInvalid:
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	w.WriteHeader(http.StatusCreated)
}
