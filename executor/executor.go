package executor

import (
	"github.com/nu7hatch/gouuid"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/vito/gordon"
	"math/rand"
	"sync"
	"time"

	Bbs "github.com/cloudfoundry-incubator/runtime-schema/bbs"
)

type Executor struct {
	id string

	bbs          Bbs.ExecutorBBS
	wardenClient gordon.Client

	runOnceGroup         *sync.WaitGroup
	stopHandlingRunOnces chan bool
}

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client) *Executor {
	uuid, err := uuid.NewV4()
	if err != nil {
		panic("Failed to generate a random guid....:" + err.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:          bbs,
		wardenClient: wardenClient,
		runOnceGroup: &sync.WaitGroup{},
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) HandleRunOnces() {
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
}

//StopHandlingRunOnces is used mainly in test to avoid having multiple executors
//running concurrently from polluting the tests
func (e *Executor) StopHandlingRunOnces() {
	//tell the watcher to stop
	e.stopHandlingRunOnces <- true
	//wait for any running runOnce goroutines to end
	e.runOnceGroup.Wait()
}

func (e *Executor) sleepForARandomInterval() {
	interval := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(100)
	time.Sleep(time.Duration(interval) * time.Millisecond)
}

func (e *Executor) runOnce(runOnce models.RunOnce) {
	defer e.runOnceGroup.Done()
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
			println("failed to clean up container: " + runOnce.ContainerHandle)
		}
	}
}
