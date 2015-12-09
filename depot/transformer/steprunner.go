package transformer

import (
	"os"

	"github.com/cloudfoundry-incubator/executor/depot/steps"
)

type StepRunner struct {
	action            steps.Step
	healthCheckPassed <-chan struct{}
}

func newStepRunner(action steps.Step, healthCheckPassed <-chan struct{}) *StepRunner {
	return &StepRunner{action: action, healthCheckPassed: healthCheckPassed}
}

func (p *StepRunner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	resultCh := make(chan error)
	go func() {
		resultCh <- p.action.Perform()
	}()

	<-p.healthCheckPassed
	close(ready)

	for {
		select {
		case <-signals:
			p.action.Cancel()
			signals = nil
		case err := <-resultCh:
			return err
		}
	}
}
