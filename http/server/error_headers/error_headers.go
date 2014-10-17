package error_headers

import (
	"net/http"

	"github.com/cloudfoundry-incubator/executor"
)

func Write(err error, w http.ResponseWriter) {
	switch v := err.(type) {
	case executor.Error:
		w.Header().Set("X-Executor-Error", v.Name())
		w.WriteHeader(v.HttpCode())
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}
