package steps

import (
	"code.cloudfoundry.org/workpool"
)

type throttleStep struct {
	substep  Step
	workPool *workpool.WorkPool
}

func NewThrottle(substep Step, workPool *workpool.WorkPool) *throttleStep {
	return &throttleStep{
		substep:  substep,
		workPool: workPool,
	}
}

func (step *throttleStep) Perform() error {
	stepResult := make(chan error)

	step.workPool.Submit(func() {
		stepResult <- step.substep.Perform()
	})

	return <-stepResult
}

func (step *throttleStep) Cancel() {
	step.substep.Cancel()
}
