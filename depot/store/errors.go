package store

import "fmt"

type InvalidStateError struct {
	State string
}

func (err InvalidStateError) Error() string {
	return fmt.Sprintf("invalid state: %s", err.State)
}

type InvalidHealthError struct {
	Health string
}

func (err InvalidHealthError) Error() string {
	return fmt.Sprintf("invalid health: %s", err.Health)
}

type MalformedPropertyError struct {
	Property string
	Value    string
}

func (err MalformedPropertyError) Error() string {
	return fmt.Sprintf("malformed property '%s': %s", err.Property, err.Value)
}

type InvalidJSONError struct {
	Property     string
	Value        string
	UnmarshalErr error
}

func (err InvalidJSONError) Error() string {
	return fmt.Sprintf(
		"invalid JSON in property %s: %s\n\nvalue: %s",
		err.Property,
		err.UnmarshalErr.Error(),
		err.Value,
	)
}

type ContainerLookupError struct {
	Handle    string
	LookupErr error
}

func (err ContainerLookupError) Error() string {
	return fmt.Sprintf(
		"lookup container with handle %s failed: %s",
		err.Handle,
		err.LookupErr.Error(),
	)
}
