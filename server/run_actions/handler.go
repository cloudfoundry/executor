package run_actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
)

const MAX_CALLBACK_ATTEMPTS = 42

type handler struct {
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
	waitGroup    *sync.WaitGroup
	cancelChan   chan struct{}
	logger       *gosteno.Logger
}

func New(
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
	waitGroup *sync.WaitGroup,
	cancelChan chan struct{},
	logger *gosteno.Logger,
) http.Handler {
	return &handler{
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
		waitGroup:    waitGroup,
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

	h.waitGroup.Add(1)
	go h.performRunActions(guid, container, request, sequence.New(steps), &result)

	w.WriteHeader(http.StatusCreated)
}

func (h *handler) performRunActions(guid string, container warden.Container, request api.ContainerRunRequest, seq sequence.Step, result *string) {
	defer h.waitGroup.Done()

	seqErr := h.performSequence(seq)

	if seqErr != nil {
		h.logger.Errord(map[string]interface{}{
			"error": seqErr.Error(),
		}, "executor.perform-sequence.failed")
	}

	err := h.wardenClient.Destroy(container.Handle())
	if err != nil {
		h.logger.Warnd(map[string]interface{}{
			"error":  err.Error(),
			"handle": container.Handle(),
		}, "executor.run-action.destroy-failed")
	}

	h.registry.Delete(guid)

	if request.CompleteURL == "" {
		return
	}

	var payload api.ContainerRunResult

	if seqErr != nil {
		payload.Failed = true
		payload.FailureReason = seqErr.Error()
	}
	payload.Metadata = request.Metadata
	payload.Result = *result

	resultPayload, err := json.Marshal(payload)
	if err != nil {
		h.logger.Errord(map[string]interface{}{
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

		h.logger.Warnd(map[string]interface{}{
			"error": err.Error(),
		}, "executor.run-action-callback.failed")

		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}

	h.logger.Errord(map[string]interface{}{
		"error": err.Error(),
	}, "executor.run-action-callback.callback-failed")
}

func (h *handler) performSequence(seq sequence.Step) error {
	doneChan := make(chan error)

	go func() {
		doneChan <- seq.Perform()
	}()

	select {
	case err := <-doneChan:
		return err

	case <-h.cancelChan:
		h.logger.Info("executor.perform-action.cancelled")
		seq.Cancel()
		return <-doneChan
	}
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
