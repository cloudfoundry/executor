package executor

import (
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-cf-experimental/runtime-schema/models"
	"github.com/vito/gordon"

	Bbs "github.com/pivotal-cf-experimental/runtime-schema/bbs"
)

type Executor struct {
	id string

	bbs          Bbs.ExecutorBBS
	wardenClient gordon.Client

	stopHandlingRunOnces chan<- bool
}

func New(bbs Bbs.ExecutorBBS, wardenClient gordon.Client) *Executor {
	uuid, ohgodno := uuid.NewV4()
	if ohgodno != nil {
		panic("wtf?: " + ohgodno.Error())
	}

	return &Executor{
		id: uuid.String(),

		bbs:          bbs,
		wardenClient: wardenClient,
	}
}

func (e *Executor) ID() string {
	return e.id
}

func (e *Executor) HandleRunOnces() {
	for {
		runOnces, stop, errors := e.bbs.WatchForDesiredRunOnce()

		e.stopHandlingRunOnces = stop

	INNER:
		for {
			select {
			case runOnce, ok := <-runOnces:
				if !ok {
					return
				}

				go e.runOnce(runOnce)
			case <-errors:
				break INNER
			}
		}
	}
}

func (e *Executor) StopHandlingRunOnces() {
	e.stopHandlingRunOnces <- true
}

func (e *Executor) runOnce(runOnce models.RunOnce) {
	runOnce.ExecutorID = e.id

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
