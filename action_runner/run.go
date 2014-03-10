package action_runner

type Performer func(actions ...Action) error

func Run(actions ...Action) error {
	return New(actions).Perform()
}
