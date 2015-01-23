package allocationstore

import "github.com/cloudfoundry-incubator/executor"

//go:generate counterfeiter -o fakes/fake_event_emitter.go . EventEmitter
type EventEmitter interface {
	EmitEvent(executor.Event)
}
