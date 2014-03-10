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

	bbs Bbs.ExecutorBBS

	runOnceGroup         *sync.WaitGroup
	stopHandlingRunOnces chan bool

	stopMaintainingPresence chan bool

	logger *steno.Logger
}

func New(bbs Bbs.ExecutorBBS, logger *steno.Logger) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:          bbs,
		runOnceGroup: &sync.WaitGroup{},

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

	stop := make(chan bool)

	go func() {
		select {
		case <-stop:
			presence.Remove()

		case <-maintainingPresenceErrors:
			e.logger.Error("executor.maintaining-presence.failed")
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
	stopChannel := make(chan bool, 1)

	go func() {
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

	return stopChannel
}

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}
