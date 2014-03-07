package action_runner_test

type FakeAction struct {
	perform func(result chan<- error)
	cancel  func()
	cleanup func()
}

func (fakeAction FakeAction) Perform(result chan<- error) {
	if fakeAction.perform != nil {
		fakeAction.perform(result)
	} else {
		result <- nil
	}
}

func (fakeAction FakeAction) Cancel() {
	if fakeAction.cancel != nil {
		fakeAction.cancel()
	}
}

func (fakeAction FakeAction) Cleanup() {
	if fakeAction.cleanup != nil {
		fakeAction.cleanup()
	}
}
