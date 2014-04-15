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
	) (presence Presence, disappeared <-chan bool, err error)

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

type MetricsBBS interface {
	GetAllRunOnces() ([]*models.RunOnce, error)
}

type FileServerBBS interface {
	MaintainFileServerPresence(
		heartbeatInterval time.Duration,
		fileServerURL string,
		fileServerId string,
	) (presence Presence, disappeared <-chan bool, err error)
}

type ServistryBBS interface {
	GetAvailableCC() (urls []string, err error)
	RegisterCC(msg models.CCRegistrationMessage, ttl time.Duration) error
	UnregisterCC(msg models.CCRegistrationMessage) error
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

		MetricsBBS: &metricsBBS{
			store: store,
		},

		FileServerBBS: &fileServerBBS{
			store: store,
		},

		ServistryBBS: &servistryBBS{
			store: store,
		},

		store: store,
	}
}

type BBS struct {
	ExecutorBBS
	StagerBBS
	FileServerBBS
	ServistryBBS
	MetricsBBS
	store storeadapter.StoreAdapter
}
