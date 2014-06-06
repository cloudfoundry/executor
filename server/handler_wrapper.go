package server

import (
	"net/http"
	"sync"

	"github.com/cloudfoundry/gosteno"
)

func LogAndWaitWrap(handler http.Handler, waitGroup *sync.WaitGroup, logger *gosteno.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		waitGroup.Add(1)
		defer waitGroup.Done()

		logger.Infod(map[string]interface{}{
			"method":  r.Method,
			"request": r.URL.String(),
		}, "executor.api.serving-request")

		handler.ServeHTTP(w, r)

		logger.Infod(map[string]interface{}{
			"method":  r.Method,
			"request": r.URL.String(),
		}, "executor.api.done-serving-request")

	}
}
