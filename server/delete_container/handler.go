package delete_container

import (
	"errors"
	"net/http"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/server/error_headers"
	"github.com/pivotal-golang/lager"
)

var ErrConcurrentDelete = errors.New("already deleting container")

type handler struct {
	depotClient api.Client
	logger      lager.Logger

	deletes  map[string]struct{}
	deletesL *sync.Mutex
}

func New(depotClient api.Client, logger lager.Logger) http.Handler {
	return &handler{
		depotClient: depotClient,
		logger:      logger,

		deletes:  make(map[string]struct{}),
		deletesL: new(sync.Mutex),
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	deleteLog := h.logger.Session("delete-handler")

	h.deletesL.Lock()
	_, alreadyDeleting := h.deletes[guid]
	if !alreadyDeleting {
		h.deletes[guid] = struct{}{}
	}
	h.deletesL.Unlock()

	var err error

	if alreadyDeleting {
		err = ErrConcurrentDelete
	} else {
		err = h.depotClient.DeleteContainer(guid)
	}

	if !alreadyDeleting {
		h.deletesL.Lock()
		delete(h.deletes, guid)
		h.deletesL.Unlock()
	}

	if err != nil {
		deleteLog.Error("failed-to-delete-container", err)
		error_headers.Write(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
}
