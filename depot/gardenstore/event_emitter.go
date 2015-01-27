package gardenstore

import "github.com/cloudfoundry-incubator/executor"

//go:generate counterfeiter -o fakes/fake_event_emitter.go . EventEmitter

type EventEmitter interface {
	Emit(executor.Event)
}
