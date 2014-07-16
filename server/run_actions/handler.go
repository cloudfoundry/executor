package run_actions

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
	runWaitGroup *sync.WaitGroup
	cancelChan   chan struct{}
	logger       *gosteno.Logger
}

func New(
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	runWaitGroup *sync.WaitGroup,
	cancelChan chan struct{},
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
		runWaitGroup: runWaitGroup,
		cancelChan:   cancelChan,
		logger:       logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	var request api.ContainerRunRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.invalid-request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reg, err := h.registry.FindByGuid(guid)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.container-not-found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	container, err := h.wardenClient.Lookup(reg.ContainerHandle)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.lookup-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var result string
	steps, err := h.transformer.StepsFor(reg.Log, request.Actions, container, &result)
	if err != nil {
		h.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.steps-invalid")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.runWaitGroup.Add(1)
	go executor.RunSequence(request.CompleteURL, h.runWaitGroup, h.cancelChan, h.wardenClient, h.registry, reg, h.logger, sequence.New(steps), &result)

	w.WriteHeader(http.StatusCreated)
}
