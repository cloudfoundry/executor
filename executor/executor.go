package executor

import (
	"github.com/cloudfoundry-incubator/executor/taskregistry"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/nu7hatch/gouuid"
	"github.com/vito/gordon"
	"math/rand"
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

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client, taskRegistry *taskregistry.TaskRegistry) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:          bbs,
		wardenClient: wardenClient,
		runOnceGroup: &sync.WaitGroup{},

		logger: steno.NewLogger("Executor"),

		taskRegistry: taskRegistry,
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) MaintainPresence(heartbeatInterval uint64) error {
	stopMaintainingPresence, maintainingPresenceErrors, err := e.bbs.MaintainExecutorPresence(heartbeatInterval, e.ID())
	if err != nil {
		return err
	}

	stop := make(chan bool)

	go func() {
		select {
		case <-stop:
			stopMaintainingPresence <- true

		case <-maintainingPresenceErrors:
			e.logger.Warn("executor.maintaining-presence.failed")
			close(e.stopHandlingRunOnces)
		}
	}()

	e.stopMaintainingPresence = stop

	return nil
}

func (e *Executor) HandleRunOnces() error {
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

					go e.runOnce(runOnce)
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
func (e *Executor) StopHandlingRunOnces() {
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

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}

func (e *Executor) runOnce(runOnce models.RunOnce) {
	defer e.runOnceGroup.Done()
	if !e.taskRegistry.AddRunOnce(runOnce) {
		return
	}
	defer e.taskRegistry.RemoveRunOnce(runOnce)

	runOnce.ExecutorID = e.id

	e.sleepForARandomInterval()

	err := e.bbs.ClaimRunOnce(runOnce)
	if err != nil {
		return
	}

	createResponse, err := e.wardenClient.Create()
	if err != nil {
		return
	}

	runOnce.ContainerHandle = createResponse.GetHandle()

	err = e.bbs.StartRunOnce(runOnce)
	if err != nil {
		_, err := e.wardenClient.Destroy(runOnce.ContainerHandle)
		if err != nil {
			e.logger.Errord(map[string]interface{}{
				"ContainerHandle": runOnce.ContainerHandle,
			}, "failed to destroy container")
		}
	}

	//standin for actually doing things
	numberOfActions := len(runOnce.Actions)
	if numberOfActions > 0 {
		time.Sleep(time.Duration(numberOfActions) * time.Second)
	}

	defer e.bbs.CompleteRunOnce(runOnce)
}

func (e *Executor) ConvergeRunOnces(period time.Duration) chan<- bool {
	e.converge(period)

	stopChannel := make(chan bool, 1)

	go func() {
		ticker := time.NewTicker(period)

		for {
			select {
			case <-ticker.C:
				e.converge(period)

			case <-stopChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return stopChannel
}

func (e *Executor) converge(period time.Duration) {
	success, err := e.bbs.GrabRunOnceLock(period)

	if err != nil {
		e.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "error when grabbing converge lock")
	} else if success {
		e.bbs.ConvergeRunOnce()
	}
}
