package action_runner

type Action interface {
	Perform() error
	Cancel()
	Cleanup()
}
