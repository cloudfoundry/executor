package depot

import (
	"encoding/json"
	"os"

	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/exchanger"
	"github.com/cloudfoundry-incubator/executor/depot/sequence"
	gapi "github.com/cloudfoundry-incubator/garden/api"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
)

type RunSequence struct {
	Container    gapi.Container
	CompleteURL  string
	Registration executor.Container
	Sequence     sequence.Step
	Result       *string
	Logger       lager.Logger
}

func (r RunSequence) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	seqComplete := make(chan error)

	runLog := r.Logger.Session("run", lager.Data{
		"guid": r.Registration.Guid,
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

			result := executor.ContainerRunResult{
				Guid: r.Registration.Guid,
			}

			if seqErr == nil {
				result.Failed = false
			} else {
				result.Failed = true
				result.FailureReason = seqErr.Error()
			}

			resultPayload, err := json.Marshal(result)
			if err != nil {
				return err
			}

			err = r.Container.SetProperty(exchanger.ContainerResultProperty, string(resultPayload))
			if err != nil {
				// not a lot we can do here
				runLog.Error("failed-to-save-result", err)
			}

			err = r.Container.SetProperty(exchanger.ContainerStateProperty, string(executor.StateCompleted))
			if err != nil {
				// not a lot we can do here
				runLog.Error("failed-to-save-result", err)
			}

			if r.CompleteURL == "" {
				return err
			}

			ifrit.Invoke(&Callback{
				URL:     r.CompleteURL,
				Payload: result,
			})

			runLog.Info("callback-started")

			return err
		}
	}
}
