package run_actions

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/cloudfoundry-incubator/executor/api/containers"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/executor/transformer"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

const MAX_CALLBACK_ATTEMPTS = 42

type handler struct {
	wardenClient warden.Client
	registry     registry.Registry
	transformer  *transformer.Transformer
}

func New(
	wardenClient warden.Client,
	registry registry.Registry,
	transformer *transformer.Transformer,
) http.Handler {
	return &handler{
		wardenClient: wardenClient,
		registry:     registry,
		transformer:  transformer,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	var request containers.ContainerRunRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		log.Println("invalid jason", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reg, err := h.registry.FindByGuid(guid)
	if err != nil {
		log.Println("container not found:", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	container, err := h.wardenClient.Lookup(reg.ContainerHandle)
	if err != nil {
		panic("well shit.")
		return
	}

	var result string
	steps := h.transformer.StepsFor(models.LogConfig{}, request.Actions, container, &result)

	go performRunActions(request.CompleteURL, sequence.New(steps), &result)

	w.WriteHeader(http.StatusCreated)
}

func performRunActions(completeURL string, seq sequence.Step, result *string) {
	err := seq.Perform()

	if completeURL == "" {
		return
	}

	var payload containers.ContainerRunResult

	if err != nil {
		payload.Failed = true
		payload.FailureReason = err.Error()
	}

	payload.Result = *result

	resultPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for i := 1; i <= MAX_CALLBACK_ATTEMPTS; i++ {
		ok := performCompleteCallback(completeURL, resultPayload)
		if ok {
			return
		}

		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}
}

func performCompleteCallback(completeURL string, payload []byte) bool {
	resultRequest, err := http.NewRequest("PUT", completeURL, bytes.NewBuffer(payload))
	if err != nil {
		log.Println("invalid callback:", completeURL)
		return false
	}

	resultRequest.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(resultRequest)
	if err != nil {
		return false
	}

	res.Body.Close()

	return res.StatusCode == http.StatusOK
}
