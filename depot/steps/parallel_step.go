package steps

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-multierror"
	"github.com/tedsuo/ifrit"
)

type parallelStep struct {
	substeps []ifrit.Runner
}

func NewParallel(substeps []ifrit.Runner) *parallelStep {
	return &parallelStep{
		substeps: substeps,
	}
}

func (step *parallelStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	var subProcesses []ifrit.Process
	for _, subStep := range step.substeps {
		subProcesses = append(subProcesses, ifrit.Background(subStep))
	}

	go waitForChildrenToBeReady(subProcesses, ready)

	aggregate := &multierror.Error{}
	aggregate.ErrorFormat = step.errorFormat

	for _, subProcess := range subProcesses {
		select {
		case s := <-signals:
			cancel(subProcesses, s)
		case err := <-subProcess.Wait():
			if err != nil && err != ErrCancelled {
				aggregate = multierror.Append(aggregate, err)
			}
		}
	}

	return aggregate.ErrorOrNil()
}

func cancel(processes []ifrit.Process, signal os.Signal) {
	for _, p := range processes {
		p.Signal(signal)
	}
}

func waitForChildrenToBeReady(processes []ifrit.Process, ready chan<- struct{}) {
	for _, p := range processes {
		select {
		case <-p.Wait():
			// do not leak goroutine if a subProcess exits before it is ready
			return
		case <-p.Ready():
		}
	}
	close(ready)
}

func (step *parallelStep) errorFormat(errs []error) string {
	var err string
	for _, e := range errs {
		if err == "" {
			err = e.Error()
		} else {
			err = fmt.Sprintf("%s; %s", err, e)
		}
	}
	return err
}
