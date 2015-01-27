package events

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/pivotal-golang/lager"
	"github.com/vito/go-sse/sse"
)

type Generator struct {
	depotClientProvider executor.ClientProvider
}

type handler struct {
	depotClient executor.Client
}

func New(depotClientProvider executor.ClientProvider) *Generator {
	return &Generator{
		depotClientProvider: depotClientProvider,
	}
}

func (generator *Generator) WithLogger(logger lager.Logger) http.Handler {
	return &handler{
		depotClient: generator.depotClientProvider.WithLogger(logger),
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	closeNotify := w.(http.CloseNotifier).CloseNotify()

	eventSource, err := h.depotClient.SubscribeToEvents()
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	defer eventSource.Close()

	flusher := w.(http.Flusher)

	w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Connection", "keep-alive")

	w.WriteHeader(http.StatusOK)

	flusher.Flush()

	go func() {
		<-closeNotify
		eventSource.Close()
	}()

	eventID := 0
	for {
		event, err := eventSource.Next()
		if err != nil {
			return
		}

		payload, err := json.Marshal(event)
		if err != nil {
			return
		}

		sse.Event{
			ID:   strconv.Itoa(eventID),
			Name: string(event.EventType()),
			Data: payload,
		}.Write(w)

		flusher.Flush()

		eventID++
	}
}
