package run_actions

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/executor"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
)

type handler struct {
	depotClient  executor.Client
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
	logger       *gosteno.Logger
}

func New(
	depotClient executor.Client,
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		depotClient:  depotClient,
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
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

	registration, err := h.registry.FindByGuid(guid)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.container-not-found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	container, err := h.wardenClient.Lookup(registration.ContainerHandle)
	if err != nil {
		h.logger.Infod(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.lookup-failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var result string
	steps, err := h.transformer.StepsFor(registration.Log, request.Actions, container, &result)
	if err != nil {
		h.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-actions.steps-invalid")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	go h.depotClient.RunSequence(request.CompleteURL, registration, sequence.New(steps), &result)

	w.WriteHeader(http.StatusCreated)
}
