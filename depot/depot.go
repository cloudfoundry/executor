package depot

import (
	"os"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/executor/registry"
	"github.com/cloudfoundry-incubator/executor/sequence"
	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/tedsuo/ifrit"
)

const ServerCloseErrMsg = "use of closed network connection"

type DepotRunAction struct {
	CompleteURL  string
	Registration api.Container
	Sequence     sequence.Step
	Result       *string
}

type Depot struct {
	containerOwnerName string
	registry           registry.Registry
	wardenClient       warden.Client
	drainTimeout       time.Duration
	logger             *steno.Logger
	stoppedChan        chan error
	runWaitGroup       *sync.WaitGroup
	runCanceller       chan struct{}
	runActions         <-chan DepotRunAction
}

func New(
	containerOwnerName string,
	registry registry.Registry,
	wardenClient warden.Client,
	drainTimeout time.Duration,
	runActions <-chan DepotRunAction,
	logger *steno.Logger,
) *Depot {
	return &Depot{
		containerOwnerName: containerOwnerName,
		registry:           registry,
		wardenClient:       wardenClient,
		drainTimeout:       drainTimeout,
		logger:             logger,
		stoppedChan:        make(chan error, 1),
		runWaitGroup:       new(sync.WaitGroup),
		runCanceller:       make(chan struct{}),
		runActions:         runActions,
	}
}

func (e *Depot) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	err := e.destroyContainers()
	if err != nil {
		return err
	}

	close(readyChan)

	for {
		select {
		case runAction := <-e.runActions:
			e.runWaitGroup.Add(1)
			go func() {
				defer e.runWaitGroup.Done()
				e.runSequence(runAction)
			}()

		case <-sigChan:
			e.logger.Info("executor.stopping")
			return nil
		}
	}
}

func (e *Depot) destroyContainers() error {
	containers, err := e.wardenClient.Containers(warden.Properties{
		"owner": e.containerOwnerName,
	})
	if err != nil {
		return err
	}

	for _, container := range containers {
		e.logger.Infod(
			map[string]interface{}{
				"handle": container.Handle(),
			},
			"executor.destroy-container",
		)
		err := e.wardenClient.Destroy(container.Handle())
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *Depot) runSequence(runAction DepotRunAction) {
	run := ifrit.Envoke(&Run{
		WardenClient: e.wardenClient,
		Registry:     e.registry,
		RunAction:    runAction,
		Logger:       e.logger,
	})

	var err error
	select {
	case <-e.runCanceller:
		run.Signal(os.Interrupt)
		err = <-run.Wait()
	case err = <-run.Wait():
	}

	if runAction.CompleteURL == "" {
		return
	}

	payload := api.ContainerRunResult{
		Guid:   runAction.Registration.Guid,
		Result: *runAction.Result,
	}
	if err != nil {
		payload.Failed = true
		payload.FailureReason = err.Error()
	}

	callback := ifrit.Envoke(&Callback{
		URL:     runAction.CompleteURL,
		Payload: payload,
		Logger:  e.logger,
	})

	<-callback.Wait()
}
