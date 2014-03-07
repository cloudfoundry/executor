package action_runner

func Run(actions []Action) <-chan error {
	result := make(chan error, 1)

	go New(actions).Perform(result)

	return result
}
