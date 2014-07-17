package ping

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor/executor"
)

type handler struct {
	depotClient executor.Client
}

func New(depotClient executor.Client) http.Handler {
	return &handler{
		depotClient: depotClient,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.depotClient.Ping()
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}
