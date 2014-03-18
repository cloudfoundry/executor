package bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

//Bulletin Board System/Store

const SchemaRoot = "/v1/"

type ExecutorBBS interface {
	MaintainExecutorPresence(
		heartbeatInterval time.Duration,
		executorID string,
	) (presence PresenceInterface, disappeared <-chan bool, err error)

	WatchForDesiredRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error)

	ClaimRunOnce(runOnce *models.RunOnce, executorID string) error
	StartRunOnce(runOnce *models.RunOnce, containerHandle string) error
	CompleteRunOnce(runOnce *models.RunOnce, failed bool, failureReason string, result string) error

	ConvergeRunOnce(timeToClaim time.Duration)
	MaintainConvergeLock(interval time.Duration, executorID string) (disappeared <-chan bool, stop chan<- chan bool, err error)
}

type StagerBBS interface {
	WatchForCompletedRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error)

	DesireRunOnce(*models.RunOnce) error
	ResolvingRunOnce(*models.RunOnce) error
	ResolveRunOnce(*models.RunOnce) error

	GetAvailableFileServer() (string, error)
}

type FileServerBBS interface {
	MaintainFileServerPresence(
		heartbeatInterval time.Duration,
		fileServerURL string,
		fileServerId string,
	) (presence PresenceInterface, disappeared <-chan bool, err error)
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) *BBS {
	return &BBS{
		ExecutorBBS: &executorBBS{
			store:        store,
			timeProvider: timeProvider,
		},

		StagerBBS: &stagerBBS{
			store:        store,
			timeProvider: timeProvider,
		},

		FileServerBBS: &fileServerBBS{
			store: store,
		},

		store: store,
	}
}

type BBS struct {
	ExecutorBBS
	StagerBBS
	FileServerBBS
	store storeadapter.StoreAdapter
}
