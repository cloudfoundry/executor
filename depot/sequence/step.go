package sequence

type Step interface {
	Perform() error
	Cancel()
}
