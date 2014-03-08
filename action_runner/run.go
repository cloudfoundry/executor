package action_runner

type Performer func(actions ...Action) (result <-chan error)

func Run(actions ...Action) <-chan error {
	result := make(chan error, 1)

	go func() {
		result <- New(actions).Perform()
	}()

	return result
}
