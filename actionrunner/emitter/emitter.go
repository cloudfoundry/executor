package emitter

import (
	"fmt"

	"github.com/cloudfoundry/loggregatorlib/emitter"
)

type Emitter interface {
	EmitStdout(message string)
	EmitStderr(message string)
}

type AppEmitter struct {
	Guid       string
	SourceName string
	Index      *int

	emitter emitter.Emitter
}

func New(loggregatorServer string, sharedSecret string, sourceName string, guid string, index *int) *AppEmitter {
	emitter, err := emitter.NewEmitter(
		loggregatorServer,
		sourceName,
		fmt.Sprintf("%d", index),
		sharedSecret,
		nil,
	)

	if err != nil {
		panic("failed to create an emitter for some reason: " + err.Error())
	}

	return &AppEmitter{
		Guid:       guid,
		SourceName: sourceName,
		Index:      index,

		emitter: emitter,
	}
}

func (e *AppEmitter) EmitStdout(message string) {
	e.emitter.Emit(e.Guid, message)
}

func (e *AppEmitter) EmitStderr(message string) {
	e.emitter.EmitError(e.Guid, message)
}
