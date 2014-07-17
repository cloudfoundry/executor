package executor

import (
	"errors"
	"os"
	"sync"
	"syscall"
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

type Executor struct {
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

var ErrDrainTimeout = errors.New("tasks did not complete within timeout")

func New(
	containerOwnerName string,
	registry registry.Registry,
	wardenClient warden.Client,
	drainTimeout time.Duration,
	runActions <-chan DepotRunAction,
	logger *steno.Logger,
) *Executor {
	return &Executor{
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

func (e *Executor) Run(sigChan <-chan os.Signal, readyChan chan<- struct{}) error {
	err := e.destroyContainers()
	if err != nil {
		return err
	}

	close(readyChan)

	stopping := false

	for {
		select {
		case runAction := <-e.runActions:
			e.runWaitGroup.Add(1)
			go e.runSequence(runAction)

		case signal := <-sigChan:
			if stopping {
				e.logger.Info("executor.signal.ignored")
				break
			}

			switch signal {
			case syscall.SIGINT, syscall.SIGTERM:
				e.logger.Info("executor.stopping")
				stopping = true
				go e.drain(0)
			case syscall.SIGUSR1:
				e.logger.Info("executor.draining")
				stopping = true
				go e.drain(e.drainTimeout)
			}

		case err := <-e.stoppedChan:
			e.logger.Info("executor.stopped")
			return err
		}
	}
}

func (e *Executor) drain(drainTimeout time.Duration) {
	time.AfterFunc(drainTimeout, func() {
		close(e.runCanceller)
		// This whole thing should be done better.
		// Stop-gap solution so deploys don't hang.
		// See: https://www.pivotaltracker.com/story/show/73640912
		e.logger.Error("executor.shutting-down.drain-timout-complete.about-to-die")
		time.Sleep(10 * time.Second)
		e.logger.Fatal("executor.shutting-down.failed-to-drain.shutting-down-violently")
	})

	e.logger.Info("executor.shutting-down.waiting-on-requests")
	// Cancelling running sequences goes here
	e.runWaitGroup.Wait()
	e.logger.Info("executor.shutting-down.done-waiting-on-requests")
	e.stoppedChan <- nil
}

func (e *Executor) destroyContainers() error {
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

func (e *Executor) runSequence(runAction DepotRunAction) {
	defer e.runWaitGroup.Done()

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
