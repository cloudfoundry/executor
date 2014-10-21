package depot

import (
	"os"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/registry"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	gapi "github.com/cloudfoundry-incubator/garden/api"
	"github.com/pivotal-golang/lager"
)

type RunSequence struct {
	Container    gapi.Container
	Registration executor.Container
	Sequence     sequence.Step
	Result       *string
	Registry     registry.Registry
	Logger       lager.Logger
}

func (r RunSequence) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	seqComplete := make(chan error)

	runLog := r.Logger.Session("run", lager.Data{
		"guid":   r.Registration.Guid,
		"handle": r.Registration.ContainerHandle,
	})

	go func() {
		runLog.Info("starting")
		seqComplete <- r.Sequence.Perform()
	}()

	close(readyChan)

	for {
		select {
		case <-sigChan:
			sigChan = nil
			r.Sequence.Cancel()
			runLog.Info("cancelled")

		case seqErr := <-seqComplete:
			if seqErr == sequence.CancelledError {
				return seqErr
			}

			runLog.Info("completed")

			payload := executor.ContainerRunResult{
				Guid: r.Registration.Guid,
			}

			// there's not a whole lot we can do about these errors.
			if seqErr == nil {
				payload.Failed = false

				err := r.Container.SetProperty(runResultFailedProperty, runResultFalseValue)
				if err != nil {
					runLog.Error("failed-to-succeed", err)
				}
			} else {
				payload.Failed = true
				payload.FailureReason = seqErr.Error()

				err := r.Container.SetProperty(runResultFailedProperty, runResultTrueValue)
				if err != nil {
					runLog.Error("failed-to-fail", err)
				}

				err = r.Container.SetProperty(runResultFailureReasonProperty, seqErr.Error())
				if err != nil {
					runLog.Error("failed-to-fail-for-a-reason", err)
				}
			}

			err := r.Registry.Complete(r.Registration.Guid, payload)
			if err != nil {
				runLog.Error("failed-to-complete", err)
			}

			return err
		}
	}
}
