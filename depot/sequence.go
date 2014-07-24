package depot

import (
	"os"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry/gosteno"
	"github.com/tedsuo/ifrit"
)

type RunSequence struct {
	CompleteURL  string
	Registration api.Container
	Sequence     sequence.Step
	Result       *string
	Registry     registry.Registry
	Logger       *gosteno.Logger
}

func (r RunSequence) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	seqComplete := make(chan error)

	go func() {
		r.Logger.Infod(map[string]interface{}{
			"guid":   r.Registration.Guid,
			"handle": r.Registration.ContainerHandle,
		}, "depot.sequence.start")
		seqComplete <- r.Sequence.Perform()
	}()

	close(readyChan)

	for {
		select {
		case <-sigChan:
			sigChan = nil
			r.Sequence.Cancel()
			r.Logger.Infod(map[string]interface{}{
				"guid":   r.Registration.Guid,
				"handle": r.Registration.ContainerHandle,
			}, "depot.sequence.cancelled")

		case err := <-seqComplete:
			r.Logger.Infod(map[string]interface{}{
				"guid":   r.Registration.Guid,
				"handle": r.Registration.ContainerHandle,
			}, "depot.sequence.complete")

			payload := api.ContainerRunResult{
				Guid:   r.Registration.Guid,
				Result: *r.Result,
			}

			if err != nil {
				payload.Failed = true
				payload.FailureReason = err.Error()
			}

			err = r.Registry.Complete(r.Registration.Guid, payload)
			if err != nil {
				r.Logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "depot.complete-container.failed")
			}

			if r.CompleteURL == "" {
				return err
			}

			ifrit.Envoke(&Callback{
				URL:     r.CompleteURL,
				Payload: payload,
			})

			r.Logger.Infod(map[string]interface{}{
				"guid":   r.Registration.Guid,
				"handle": r.Registration.ContainerHandle,
			}, "depot.sequence.callback-started")
			return err
		}
	}
}
