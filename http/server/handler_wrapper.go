package server

import (
	"net/http"

	"github.com/pivotal-golang/lager"
)

func LogWrap(provider HandlerProvider, logger lager.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestLog := logger.Session("request", lager.Data{
			"method":  r.Method,
			"request": r.URL.String(),
		})

		requestLog.Info("serving")

		provider.WithLogger(requestLog).ServeHTTP(w, r)

		requestLog.Info("done")
	}
}
