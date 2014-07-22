package api

import "net/http"

type Error interface {
	error

	StatusCode() int
}

type execError struct {
	message    string
	statusCode int
}

func (err execError) Error() string {
	return err.message
}

func (err execError) StatusCode() int {
	return err.statusCode
}

var Errors = map[int]Error{}

func registerError(message string, status int) Error {
	err := execError{message, status}
	Errors[status] = err
	return err
}

var (
	ErrContainerGuidNotAvailable      = registerError("container guid not available", http.StatusBadRequest)
	ErrInsufficientResourcesAvailable = registerError("insufficient resources available", http.StatusServiceUnavailable)
	ErrContainerNotFound              = registerError("container not found", http.StatusNotFound)
	ErrStepsInvalid                   = registerError("steps invalid", http.StatusBadRequest)
	ErrLimitsInvalid                  = registerError("container limits invalid", http.StatusBadRequest)
)
