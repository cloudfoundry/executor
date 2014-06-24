package ping

import (
	"net/http"

	"github.com/cloudfoundry-incubator/garden/warden"
)

type handler struct {
	wardenClient warden.Client
}

func New(wardenClient warden.Client) http.Handler {
	return &handler{
		wardenClient: wardenClient,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.wardenClient.Ping()
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}
