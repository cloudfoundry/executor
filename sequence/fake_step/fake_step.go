package fake_step

type FakeStep struct {
	WhenPerforming func() error
	WhenCancelling func()
	WhenCleaningUp func()
}

func (fakeStep FakeStep) Perform() error {
	if fakeStep.WhenPerforming != nil {
		return fakeStep.WhenPerforming()
	}

	return nil
}

func (fakeStep FakeStep) Cancel() {
	if fakeStep.WhenCancelling != nil {
		fakeStep.WhenCancelling()
	}
}

func (fakeStep FakeStep) Cleanup() {
	if fakeStep.WhenCleaningUp != nil {
		fakeStep.WhenCleaningUp()
	}
}
