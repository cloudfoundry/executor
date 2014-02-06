package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	"time"
)

//Bulletin Board System/Store

type ExecutorBBS interface {
	MaintainExecutorPresence(heartbeatIntervalInSeconds uint64, executorID string) (chan bool, chan error, error)
	WatchForDesiredRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) //filter out delete...

	ClaimRunOnce(models.RunOnce) error
	StartRunOnce(models.RunOnce) error
	CompleteRunOnce(models.RunOnce) error

	ConvergeRunOnce() //should be executed periodically
	GrabRunOnceLock(time.Duration) (bool, error)
}

type StagerBBS interface {
	WatchForCompletedRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) //filter out delete...

	DesireRunOnce(models.RunOnce) error
	ResolveRunOnce(models.RunOnce) error
}

func New(store storeadapter.StoreAdapter) *BBS {
	return &BBS{
		ExecutorBBS: &executorBBS{store: store},
		StagerBBS:   &stagerBBS{store: store},
		store:       store,
	}
}

type BBS struct {
	ExecutorBBS
	StagerBBS
	store storeadapter.StoreAdapter
}
