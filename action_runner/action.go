package action_runner

type Action interface {
	Perform(result chan<- error)
	Cancel()
	Cleanup()
}
