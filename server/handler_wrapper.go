package server

import (
	"net/http"

	"github.com/pivotal-golang/lager"
)

func LogWrap(handler http.Handler, logger lager.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestLog := logger.Session("request", lager.Data{
			"method":  r.Method,
			"request": r.URL.String(),
		})

		requestLog.Debug("serving")

		handler.ServeHTTP(w, r)

		requestLog.Debug("done")
	}
}
