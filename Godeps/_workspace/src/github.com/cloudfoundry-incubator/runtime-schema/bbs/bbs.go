package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	"time"
)

//Bulletin Board System/Store

const SchemaRoot = "/v1/"
const DefaultPendingTimeout = 30 * time.Minute

type ExecutorBBS interface {
	SetTimeToClaim(timeToClaim time.Duration)

	MaintainExecutorPresence(heartbeatIntervalInSeconds uint64, executorID string) (PresenceInterface, chan error, error)
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

	GetAvailableFileServer() (string, error)
}

type FileServerBBS interface {
	MaintainFileServerPresence(heartbeatIntervalInSeconds uint64, fileServerURL string, fileServerId string) (PresenceInterface, chan error, error)
}

func New(store storeadapter.StoreAdapter) *BBS {
	return &BBS{
		ExecutorBBS: &executorBBS{
			store:       store,
			timeToClaim: DefaultPendingTimeout,
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
