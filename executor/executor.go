package executor

import (
	"github.com/cloudfoundry-incubator/executor/runoncehandler"
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	"math/rand"

	"github.com/nu7hatch/gouuid"
	"github.com/vito/gordon"
	"sync"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	steno "github.com/cloudfoundry/gosteno"
)

type Executor struct {
	id string

	bbs          Bbs.ExecutorBBS
	wardenClient gordon.Client

	runOnceGroup         *sync.WaitGroup
	stopHandlingRunOnces chan bool

	stopMaintainingPresence chan bool

	logger *steno.Logger

	taskRegistry *taskregistry.TaskRegistry
}

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client, taskRegistry *taskregistry.TaskRegistry, logger *steno.Logger) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:          bbs,
		wardenClient: wardenClient,
		runOnceGroup: &sync.WaitGroup{},

		logger: logger,

		taskRegistry: taskRegistry,
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) MaintainPresence(heartbeatInterval uint64) error {
	presence, maintainingPresenceErrors, err := e.bbs.MaintainExecutorPresence(heartbeatInterval, e.ID())
	if err != nil {
		return err
	}

	stop := make(chan bool)

	go func() {
		select {
		case <-stop:
			presence.Remove()

		case <-maintainingPresenceErrors:
			e.logger.Warn("executor.maintaining-presence.failed")
			close(e.stopHandlingRunOnces)
		}
	}()

	e.stopMaintainingPresence = stop

	return nil
}

func (e *Executor) Handle(runOnceHandler runoncehandler.RunOnceHandlerInterface) error {
	ready := make(chan bool)
	e.stopHandlingRunOnces = make(chan bool)

	go func() {
		runOnces, stop, errors := e.bbs.WatchForDesiredRunOnce()
		ready <- true

		for {
		INNER:
			for {
				select {
				case runOnce, ok := <-runOnces:
					if !ok {
						return
					}

					e.runOnceGroup.Add(1)

					go func() {
						e.sleepForARandomInterval()
						runOnceHandler.RunOnce(runOnce, e.id)
						e.runOnceGroup.Done()
					}()
				case <-e.stopHandlingRunOnces:
					stop <- true
					return
				case <-errors:
					break INNER
				}
			}

			runOnces, stop, errors = e.bbs.WatchForDesiredRunOnce()
		}
	}()

	<-ready
	return nil
}

//StopHandlingRunOnces is used mainly in test to avoid having multiple executors
//running concurrently from polluting the tests
func (e *Executor) StopHandling() {
	// stop maintaining our presence
	if e.stopMaintainingPresence != nil {
		close(e.stopMaintainingPresence)
	}

	//tell the watcher to stop
	if e.stopHandlingRunOnces != nil {
		close(e.stopHandlingRunOnces)
	}

	//wait for any running runOnce goroutines to end
	e.runOnceGroup.Wait()
}

func (e *Executor) ConvergeRunOnces(period time.Duration, timeToClaim time.Duration) chan<- bool {
	e.converge(period, timeToClaim)

	stopChannel := make(chan bool, 1)

	go func() {
		ticker := time.NewTicker(period)

		for {
			select {
			case <-ticker.C:
				e.converge(period, timeToClaim)

			case <-stopChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return stopChannel
}

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}

func (e *Executor) converge(period time.Duration, timeToClaim time.Duration) {
	success, err := e.bbs.GrabRunOnceLock(period)

	if err != nil {
		e.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "error when grabbing converge lock")
	} else if success {
		e.bbs.ConvergeRunOnce(timeToClaim)
		e.logger.Info("Converged RunOnce")
	}
}
