package create_volume

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor"
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
	volume := executor.Volume{}
	err := json.NewDecoder(r.Body).Decode(&volume)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = h.depotClient.CreateVolume(volume)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
