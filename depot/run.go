package depot

import (
	"os"

	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/cloudfoundry/gosteno"
)

type Run struct {
	WardenClient warden.Client
	Registry     registry.Registry
	Logger       *gosteno.Logger
	RunAction    DepotRunAction
}

func (r *Run) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	seqComplete := make(chan error)

	go func() {
		seqComplete <- r.RunAction.Sequence.Perform()
	}()

	close(readyChan)

	for {
		select {
		case <-sigChan:
			r.Logger.Info("executor.perform-action.cancelled")
			sigChan = nil
			r.RunAction.Sequence.Cancel()

		case seqErr := <-seqComplete:
			if seqErr != nil {
				r.Logger.Errord(map[string]interface{}{
					"error": seqErr.Error(),
				}, "executor.perform-sequence.failed")
			}

			err := r.WardenClient.Destroy(r.RunAction.Registration.ContainerHandle)
			if err != nil {
				r.Logger.Warnd(map[string]interface{}{
					"error":  err.Error(),
					"handle": r.RunAction.Registration.ContainerHandle,
				}, "executor.run-action.destroy-failed")
			}

			r.Registry.Delete(r.RunAction.Registration.Guid)

			return seqErr
		}
	}
}
