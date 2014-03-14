package executor

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/nu7hatch/gouuid"

	"github.com/cloudfoundry-incubator/executor/runoncehandler"
)

type Executor struct {
	id string

	bbs Bbs.ExecutorBBS

	outstandingTasks *sync.WaitGroup

	stopHandlingRunOnces    chan error
	stopMaintainingPresence chan bool
	stopConvergeRunOnce     chan bool

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

func (e *Executor) MaintainPresence(heartbeatInterval time.Duration) error {
	presence, maintainingPresenceErrors, err := e.bbs.MaintainExecutorPresence(heartbeatInterval, e.ID())
	if err != nil {
		return err
	}

	e.outstandingTasks.Add(1)

	stop := make(chan bool)

	go func() {
		defer e.outstandingTasks.Done()

		select {
		case <-stop:
			presence.Remove()

		case <-maintainingPresenceErrors:
			e.logger.Error("executor.maintaining-presence.failed")
			if e.stopHandlingRunOnces != nil {
				e.stopHandlingRunOnces <- MaintainPresenceError
			}
		}
	}()

	e.stopMaintainingPresence = stop

	return nil
}

func (e *Executor) Handle(runOnceHandler runoncehandler.RunOnceHandlerInterface, ready chan<- bool) error {
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

//StopHandlingRunOnces is used mainly in test to avoid having multiple executors
//running concurrently from polluting the tests
func (e *Executor) Stop() {
	// stop maintaining our presence
	if e.stopMaintainingPresence != nil {
		close(e.stopMaintainingPresence)
		e.stopMaintainingPresence = nil
	}

	//tell the watcher to stop
	if e.stopHandlingRunOnces != nil {
		close(e.stopHandlingRunOnces)
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
	e.outstandingTasks.Add(1)

	go func() {
		defer e.outstandingTasks.Done()

		for {
			lostLock, releaseLock, err := e.bbs.MaintainConvergeLock(period, e.ID())
			if err != nil {
				e.logger.Debugd(map[string]interface{}{
					"error": err.Error(),
				}, "error when maintaining converge lock")

				time.Sleep(1 * time.Second)
				continue
			}

			e.bbs.ConvergeRunOnce(timeToClaim)

			ticker := time.NewTicker(period)

		dance:
			for {
				select {
				case <-ticker.C:
					e.bbs.ConvergeRunOnce(timeToClaim)

				case <-lostLock:
					ticker.Stop()
					break dance

				case <-stopChannel:
					ticker.Stop()
					releaseLock <- make(chan bool)
					return
				}
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
