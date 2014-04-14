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

	outstandingTasks    *sync.WaitGroup
	outstandingPresence *sync.WaitGroup
	outstandingConverge *sync.WaitGroup

	stopHandlingRunOnces    chan struct{}
	cancelRunningTasks      chan error
	stopConvergeRunOnce     chan struct{}
	stopMaintainingPresence chan struct{}

	drainTimeout time.Duration

	logger *steno.Logger

	closeOnce *sync.Once
}

var ErrLostPresence = errors.New("failed to maintain presence")
var ErrDrainTimeout = errors.New("tasks did not complete within timeout")

func New(bbs Bbs.ExecutorBBS, drainTimeout time.Duration, logger *steno.Logger) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs: bbs,

		outstandingTasks:    &sync.WaitGroup{},
		outstandingPresence: &sync.WaitGroup{},
		outstandingConverge: &sync.WaitGroup{},

		drainTimeout: drainTimeout,

		logger: logger,

		closeOnce: new(sync.Once),

		stopHandlingRunOnces:    make(chan struct{}, 2),
		cancelRunningTasks:      make(chan error, 2),
		stopConvergeRunOnce:     make(chan struct{}),
		stopMaintainingPresence: make(chan struct{}),
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) MaintainPresence(heartbeatInterval time.Duration, ready chan<- error) {
	e.outstandingPresence.Add(1)
	defer e.outstandingPresence.Done()

	presence, statusChannel, err := e.bbs.MaintainExecutorPresence(heartbeatInterval, e.ID())

	if err != nil {
		ready <- err
		return
	}

	go func() {
		<-e.stopMaintainingPresence
		presence.Remove()
	}()

	sentReady := false

	for {
		locked, ok := <-statusChannel

		if locked && !sentReady {
			ready <- nil
			sentReady = true
		}

		if !locked && ok {
			e.logger.Error("executor.maintaining-presence.failed")
			e.stopHandlingRunOnces <- struct{}{}
			e.cancelRunningTasks <- ErrLostPresence
		}

		if !ok {
			break
		}
	}
}

func (e *Executor) Handle(runOnceHandler run_once_handler.RunOnceHandlerInterface, ready chan<- bool) error {
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

			case <-e.stopHandlingRunOnces:
				stop <- true

				err := <-e.cancelRunningTasks
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

func (e *Executor) Drain() {
	e.stopHandlingRunOnces <- struct{}{}

	e.logger.Infod(
		map[string]interface{}{
			"timeout": e.drainTimeout.String(),
		},
		"executor.draining",
	)

	doneWaiting := make(chan struct{})
	go func() {
		e.outstandingTasks.Wait()
		close(doneWaiting)
	}()

	select {
	case <-doneWaiting:
		e.cancelRunningTasks <- nil

		close(e.stopMaintainingPresence)
		close(e.stopConvergeRunOnce)

		e.outstandingPresence.Wait()
		e.outstandingConverge.Wait()
	case <-time.After(e.drainTimeout):
		e.cancelRunningTasks <- ErrDrainTimeout
	}
}

func (e *Executor) Stop() {
	e.closeOnce.Do(func() {
		e.stopHandlingRunOnces <- struct{}{}
		e.cancelRunningTasks <- nil
		close(e.stopMaintainingPresence)
		close(e.stopConvergeRunOnce)
	})

	//wait for any running runOnce goroutines to end
	e.outstandingTasks.Wait()
	e.outstandingPresence.Wait()
	e.outstandingConverge.Wait()
}

func (e *Executor) ConvergeRunOnces(period time.Duration, timeToClaim time.Duration) {
	e.outstandingConverge.Add(1)
	defer e.outstandingConverge.Done()

	statusChannel, releaseLock, err := e.bbs.MaintainConvergeLock(period, e.ID())

	if err != nil {
		e.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "error when creating converge lock")
		return
	}

	go func() {
		<-e.stopConvergeRunOnce
		close(releaseLock)
	}()

	for {
		locked, ok := <-statusChannel
		if !ok {
			return
		}

		if locked {
			e.bbs.ConvergeRunOnce(timeToClaim)
		}
	}
}

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}
