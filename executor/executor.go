package executor

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/nu7hatch/gouuid"

	"github.com/cloudfoundry-incubator/executor/run_once_handler"
)

type Executor struct {
	id string

	bbs Bbs.ExecutorBBS

	outstandingTasks *sync.WaitGroup

	stopHandlingRunOnces chan error
	stopConvergeRunOnce  chan bool
	presence             Bbs.Presence

	logger *steno.Logger
}

var MaintainPresenceError = errors.New("failed to maintain presence")

func New(bbs Bbs.ExecutorBBS, logger *steno.Logger) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:              bbs,
		outstandingTasks: &sync.WaitGroup{},

		logger: logger,
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) MaintainPresence(heartbeatInterval time.Duration, ready chan<- bool) error {
	p, statusChannel, err := e.bbs.MaintainExecutorPresence(heartbeatInterval, e.ID())
	if err != nil {
		ready <- false
		return err
	}
	e.presence = p

	e.outstandingTasks.Add(1)

	go func() {
		for {
			select {
			case locked, ok := <-statusChannel:
				if locked && ready != nil {
					ready <- true
					ready = nil
				}

				if !locked && ok {

					e.logger.Error("executor.maintaining-presence.failed")
					if e.stopHandlingRunOnces != nil {
						e.stopHandlingRunOnces <- MaintainPresenceError
						e.stopHandlingRunOnces = nil
					}
				}

				if !ok {
					e.outstandingTasks.Done()
					return
				}
			}
		}
	}()

	return nil
}

func (e *Executor) Handle(runOnceHandler run_once_handler.RunOnceHandlerInterface, ready chan<- bool) error {
	e.stopHandlingRunOnces = make(chan error)
	cancel := make(chan struct{})

	e.logger.Info("executor.watching-for-desired-runonce")
	runOnces, stop, errors := e.bbs.WatchForDesiredRunOnce()
	ready <- true

	for {
	INNER:
		for {
			select {
			case runOnce, ok := <-runOnces:
				if !ok {
					break INNER
				}

				e.outstandingTasks.Add(1)

				go func() {
					defer e.outstandingTasks.Done()

					e.sleepForARandomInterval()
					runOnceHandler.RunOnce(runOnce, e.id, cancel)
				}()
			case err := <-e.stopHandlingRunOnces:
				stop <- true

				close(cancel)
				return err
			case err, ok := <-errors:
				if ok && err != nil {
					e.logger.Errord(map[string]interface{}{
						"error": err.Error(),
					}, "executor.watch-desired-runonce.failed")
				}
				break INNER
			}
		}

		e.logger.Info("executor.watching-for-desired-runonce")
		runOnces, stop, errors = e.bbs.WatchForDesiredRunOnce()
	}

	return nil
}

func (e *Executor) Stop() {
	// stop maintaining our presence
	if e.presence != nil {
		e.presence.Remove()
		e.presence = nil
	}

	if e.stopHandlingRunOnces != nil {
		e.stopHandlingRunOnces <- nil
		e.stopHandlingRunOnces = nil
	}

	if e.stopConvergeRunOnce != nil {
		close(e.stopConvergeRunOnce)
		e.stopConvergeRunOnce = nil
	}

	//wait for any running runOnce goroutines to end
	e.outstandingTasks.Wait()
}

func (e *Executor) ConvergeRunOnces(period time.Duration, timeToClaim time.Duration) chan<- bool {
	stopChannel := make(chan bool, 1)

	statusChannel, releaseLock, err := e.bbs.MaintainConvergeLock(period, e.ID())
	if err != nil {
		e.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "error when creating converge lock")
		return nil
	}

	e.outstandingTasks.Add(1)
	go func() {
		for {
			select {
			case locked, ok := <-statusChannel:
				if !ok {
					e.outstandingTasks.Done()
					return
				}

				if locked {
					e.bbs.ConvergeRunOnce(timeToClaim)
				}
			case <-stopChannel:
				stopChannel = nil
				releaseLock <- nil
			}
		}
	}()

	e.stopConvergeRunOnce = stopChannel
	return stopChannel
}

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}
