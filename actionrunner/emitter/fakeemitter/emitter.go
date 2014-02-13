package fakeemitter

type FakeEmitter struct {
	EmittedStdout []string
	EmittedStderr []string
}

func New() *FakeEmitter {
	return &FakeEmitter{}
}

func (e *FakeEmitter) EmitStdout(message string) {
	e.EmittedStdout = append(e.EmittedStdout, message)
}

func (e *FakeEmitter) EmitStderr(message string) {
	e.EmittedStderr = append(e.EmittedStderr, message)
}
