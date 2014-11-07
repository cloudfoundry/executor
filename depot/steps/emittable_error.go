package steps

import "fmt"

type EmittableError struct {
	msg          string
	wrappedError error
}

func NewEmittableError(wrappedError error, message string, args ...interface{}) *EmittableError {
	msg := message
	if len(args) > 0 {
		msg = fmt.Sprintf(message, args...)
	}

	return &EmittableError{
		wrappedError: wrappedError,
		msg:          msg,
	}
}

func (e *EmittableError) Error() string {
	if e.wrappedError == nil {
		return e.EmittableError()
	}

	return fmt.Sprintf("%s\n%s", e.msg, e.wrappedError.Error())
}

func (e *EmittableError) EmittableError() string {
	return e.msg
}
