package remaining_resources

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry/gosteno"
)

type Handler struct {
	depotClient executor.Client
	logger      *gosteno.Logger
}

func New(depotClient executor.Client, logger *gosteno.Logger) *Handler {
	return &Handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resources, err := h.depotClient.RemainingResources()

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
		}, "executor.remaining-resources.writing-body-failed")
		return
	}
	h.logger.Info("executor.remaining-resources.ok")
}
