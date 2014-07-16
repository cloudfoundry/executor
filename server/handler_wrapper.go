package server

import (
	"net/http"

	"github.com/cloudfoundry/gosteno"
)

func LogWrap(handler http.Handler, logger *gosteno.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
