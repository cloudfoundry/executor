package api

import "net/http"

type Error interface {
	error

	Name() string
	HttpCode() int
}

type execError struct {
	name     string
	message  string
	httpCode int
}

func (err execError) Name() string {
	return err.name
}

func (err execError) Error() string {
	return err.message
}

func (err execError) HttpCode() int {
	return err.httpCode
}

var Errors = map[string]Error{}

func registerError(name string, message string, status int) Error {
	err := execError{name, message, status}
	Errors[name] = err
	return err
}

var (
	ErrContainerGuidNotAvailable      = registerError("ContainerGuidNotAvailable", "container guid not available", http.StatusBadRequest)
	ErrInsufficientResourcesAvailable = registerError("InsufficientResourcesAvailable", "insufficient resources available", http.StatusServiceUnavailable)
	ErrContainerNotFound              = registerError("ContainerNotFound", "container not found", http.StatusNotFound)
	ErrStepsInvalid                   = registerError("StepsInvalid", "steps invalid", http.StatusBadRequest)
	ErrLimitsInvalid                  = registerError("LimitsInvalid", "container limits invalid", http.StatusBadRequest)
)
