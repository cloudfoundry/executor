package fake_action

type FakeAction struct {
	WhenPerforming func() error
	WhenCancelling func()
	WhenCleaningUp func()
}

func (fakeAction FakeAction) Perform() error {
	if fakeAction.WhenPerforming != nil {
		return fakeAction.WhenPerforming()
	}

	return nil
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
