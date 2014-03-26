package lazy_sequence

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor/sequence"
)

type StepGenerator func() []sequence.Step

type LazySequence struct {
	stepGenerator StepGenerator

	cancelled bool

	sequence *sequence.Sequence

	actionMutex *sync.Mutex
}

func New(stepGenerator StepGenerator) sequence.Step {
	return &LazySequence{
		stepGenerator: stepGenerator,

		actionMutex: &sync.Mutex{},
	}
}

func (lazySequence *LazySequence) Perform() error {
	lazySequence.actionMutex.Lock()

	if lazySequence.cancelled {
		lazySequence.actionMutex.Unlock()
		return sequence.CancelledError
	}

	lazySequence.sequence = sequence.New(lazySequence.stepGenerator())
	lazySequence.actionMutex.Unlock()

	return lazySequence.sequence.Perform()
}

func (lazySequence *LazySequence) Cancel() {
	lazySequence.actionMutex.Lock()
	action := lazySequence.sequence
	lazySequence.cancelled = true
	lazySequence.actionMutex.Unlock()

	if action != nil {
		action.Cancel()
	}
}

func (lazySequence *LazySequence) Cleanup() {
	lazySequence.actionMutex.Lock()
	action := lazySequence.sequence
	lazySequence.actionMutex.Unlock()

	if action != nil {
		action.Cleanup()
	}
}
