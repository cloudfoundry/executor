package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	"time"
)

//Bulletin Board System/Store

const SchemaRoot = "/v1/"

type ExecutorBBS interface {
	MaintainExecutorPresence(
		heartbeatIntervalInSeconds uint64,
		executorID string,
	) (presence PresenceInterface, disappeared <-chan bool, err error)

	WatchForDesiredRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error)

	ClaimRunOnce(models.RunOnce) error
	StartRunOnce(models.RunOnce) error
	CompleteRunOnce(models.RunOnce) error

	ConvergeRunOnce(timeToClaim time.Duration)
	MaintainConvergeLock(interval time.Duration, executorID string) (disappeared <-chan bool, stop chan<- chan bool, err error)
}

type StagerBBS interface {
	WatchForCompletedRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error)

	DesireRunOnce(models.RunOnce) error
	ResolveRunOnce(models.RunOnce) error

	GetAvailableFileServer() (string, error)
}

type FileServerBBS interface {
	MaintainFileServerPresence(
		heartbeatIntervalInSeconds uint64,
		fileServerURL string,
		fileServerId string,
	) (presence PresenceInterface, disappeared <-chan bool, err error)
}

func New(store storeadapter.StoreAdapter) *BBS {
	return &BBS{
		ExecutorBBS: &executorBBS{
			store: store,
		},
		StagerBBS:     &stagerBBS{store: store},
		FileServerBBS: &fileServerBBS{store: store},
		store:         store,
	}
}

type BBS struct {
	ExecutorBBS
	StagerBBS
	FileServerBBS
	store storeadapter.StoreAdapter
}
