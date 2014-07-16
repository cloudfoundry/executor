package executor

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	steno "github.com/cloudfoundry/gosteno"
)

const ServerCloseErrMsg = "use of closed network connection"

type Executor struct {
	containerOwnerName string
	wardenClient       warden.Client
	drainTimeout       time.Duration
	logger             *steno.Logger
	stoppedChan        chan error
	runWaitGroup       *sync.WaitGroup
	runCanceller       chan struct{}
}

var ErrDrainTimeout = errors.New("tasks did not complete within timeout")

func New(
	containerOwnerName string,
	wardenClient warden.Client,
	drainTimeout time.Duration,
	runWaitGroup *sync.WaitGroup,
	runCanceller chan struct{},
	logger *steno.Logger,
) *Executor {
	return &Executor{
		containerOwnerName: containerOwnerName,
		wardenClient:       wardenClient,
		drainTimeout:       drainTimeout,
		logger:             logger,
		stoppedChan:        make(chan error, 1),
		runWaitGroup:       runWaitGroup,
		runCanceller:       runCanceller,
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
