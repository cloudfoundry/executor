package run_actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry-incubator/runtime-schema/models/executor_api"
	"github.com/cloudfoundry/gosteno"
)

const MAX_CALLBACK_ATTEMPTS = 42

type handler struct {
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
	logger       *gosteno.Logger
}

func New(
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
		logger:       logger,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	var request executor_api.ContainerRunRequest
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
		log.Println("container not found:", err)
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
	steps := h.transformer.StepsFor(models.LogConfig{}, request.Actions, container, &result)

	go performRunActions(request, sequence.New(steps), &result, h.logger)

	w.WriteHeader(http.StatusCreated)
}

func performRunActions(request executor_api.ContainerRunRequest, seq sequence.Step, result *string, logger *gosteno.Logger) {
	err := seq.Perform()

	if request.CompleteURL == "" {
		return
	}

	var payload executor_api.ContainerRunResult

	if err != nil {
		payload.Failed = true
		payload.FailureReason = err.Error()
	}
	payload.Metadata = request.Metadata
	payload.Result = *result

	resultPayload, err := json.Marshal(payload)
	if err != nil {
		logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-action-callback.json-marshal-failed")
		return
	}

	for i := 1; i <= MAX_CALLBACK_ATTEMPTS; i++ {
		err = performCompleteCallback(request.CompleteURL, resultPayload)
		// break if we succeed
		if err == nil {
			return
		}

		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}

	logger.Errord(map[string]interface{}{
		"error": err.Error(),
	}, "executor.run-action-callback.callback-failed")
}

func performCompleteCallback(completeURL string, payload []byte) error {
	resultRequest, err := http.NewRequest("PUT", completeURL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	resultRequest.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(resultRequest)
	if err != nil {
		return err
	}

	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Callback failed with status code %d", res.StatusCode)
	}

	return nil
}
