package fake_action

type FakeAction struct {
	WhenPerforming func(result chan<- error)
	WhenCancelling func()
	WhenCleaningUp func()
}

func (fakeAction FakeAction) Perform(result chan<- error) {
	if fakeAction.WhenPerforming != nil {
		fakeAction.WhenPerforming(result)
	} else {
		result <- nil
	}
}

func (fakeAction FakeAction) Cancel() {
	if fakeAction.WhenCancelling != nil {
		fakeAction.WhenCancelling()
	}
}

func (fakeAction FakeAction) Cleanup() {
	if fakeAction.WhenCleaningUp != nil {
		fakeAction.WhenCleaningUp()
	}
}
