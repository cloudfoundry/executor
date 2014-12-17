package steps

type CancelError struct {
	WrappedError error
}

func (ce CancelError) Error() string {
	return "cancelled; " + ce.WrappedError.Error()
}
