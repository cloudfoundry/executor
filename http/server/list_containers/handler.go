package list_containers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/http/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type handler struct {
	depotClient executor.Client
	logger      lager.Logger
}

func New(depotClient executor.Client, logger lager.Logger) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	listLog := h.logger.Session("list-handler")

	tags := executor.Tags{}

	for _, tag := range r.URL.Query()["tag"] {
		segments := strings.SplitN(tag, ":", 2)
		if len(segments) == 2 {
			tags[segments[0]] = segments[1]
		}
	}

	resources, err := h.depotClient.ListContainers(tags)
	if err != nil {
		listLog.Error("failed-to-list-container", err)
		error_headers.Write(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err = json.NewEncoder(w).Encode(resources)
	if err != nil {
		listLog.Error("failed-to-marshal-response", err)
		return
	}
}
