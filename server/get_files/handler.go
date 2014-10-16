package get_files

import (
	"io"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/server/error_headers"
	"github.com/pivotal-golang/lager"
)

type handler struct {
	depotClient api.Client
	logger      lager.Logger
}

func New(depotClient api.Client, logger lager.Logger) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")
	sourcePath := r.URL.Query().Get("source")

	getLog := h.logger.Session("get-files-handler")

	stream, err := h.depotClient.GetFiles(guid, sourcePath)
	if err != nil {
		getLog.Error("failed-to-get-container", err)
		error_headers.Write(err, w)
		return
	}

	defer stream.Close()

	w.Header().Set("Content-Type", "application/x-tar")

	w.WriteHeader(http.StatusOK)

	_, err = io.Copy(w, stream)
	if err != nil {
		getLog.Error("failed-to-stream-files", err)
		return
	}
}
