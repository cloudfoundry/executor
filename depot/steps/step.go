package steps

type Step interface {
	Perform() error
	Cancel()
	Cleanup()
}
