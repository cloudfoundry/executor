package gardenstore

import "github.com/cloudfoundry-incubator/executor"

type EventEmitter interface {
	EmitEvent(executor.Event)
}
