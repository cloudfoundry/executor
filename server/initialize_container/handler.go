package initialize_container

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/server/error_headers"
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
	req := api.ContainerInitializationRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.initialize-container.bad-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	guid := r.FormValue(":guid")
	reg, err := h.depotClient.InitializeContainer(guid, req)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.failed")

		error_headers.Write(err, w)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(reg)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.init-container.writing-body-failed")
		return
	}

	h.logger.Info("executor.init-container.ok")
}
