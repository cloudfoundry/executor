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

	WatchForDesiredTask() (<-chan *models.Task, chan<- bool, <-chan error)

	ClaimTask(task *models.Task, executorID string) error
	StartTask(task *models.Task, containerHandle string) error
	CompleteTask(task *models.Task, failed bool, failureReason string, result string) error

	ConvergeTask(timeToClaim time.Duration)
	MaintainConvergeLock(interval time.Duration, executorID string) (disappeared <-chan bool, stop chan<- chan bool, err error)
}

type StagerBBS interface {
	WatchForCompletedTask() (<-chan *models.Task, chan<- bool, <-chan error)

	DesireTask(*models.Task) error
	ResolvingTask(*models.Task) error
	ResolveTask(*models.Task) error

	GetAvailableFileServer() (string, error)
}

type MetricsBBS interface {
	GetAllTasks() ([]*models.Task, error)
	GetServiceRegistrations() (models.ServiceRegistrations, error)
}

type FileServerBBS interface {
	MaintainFileServerPresence(
		heartbeatInterval time.Duration,
		fileServerURL string,
		fileServerId string,
	) (presence Presence, disappeared <-chan bool, err error)
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

		store: store,
	}
}

type BBS struct {
	ExecutorBBS
	StagerBBS
	FileServerBBS
	MetricsBBS
	store storeadapter.StoreAdapter
}
