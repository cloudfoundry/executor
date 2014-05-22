package bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/lock_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/lrp_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/services_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/task_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

//Bulletin Board System/Store

type ExecutorBBS interface {
	//services
	MaintainExecutorPresence(
		heartbeatInterval time.Duration,
		executorID string,
	) (presence services_bbs.Presence, disappeared <-chan bool, err error)
}

type RepBBS interface {
	//services
	MaintainRepPresence(heartbeatInterval time.Duration, repPresence models.RepPresence) (services_bbs.Presence, <-chan bool, error)

	//task
	WatchForDesiredTask() (<-chan models.Task, chan<- bool, <-chan error)
	ClaimTask(task models.Task, executorID string) (models.Task, error)
	StartTask(task models.Task, containerHandle string) (models.Task, error)
	CompleteTask(task models.Task, failed bool, failureReason string, result string) (models.Task, error)

	///
	ReportLongRunningProcessAsRunning(lrp models.LRP) error
}

type ConvergerBBS interface {
	//task
	ConvergeTask(timeToClaim time.Duration, converganceInterval time.Duration)

	//lock
	MaintainConvergeLock(interval time.Duration, executorID string) (disappeared <-chan bool, stop chan<- chan bool, err error)
}

type AppManagerBBS interface {
	//lrp
	DesireLongRunningProcess(models.DesiredLRP) error
	RequestLRPStartAuction(models.LRPStartAuction) error

	//services
	GetAvailableFileServer() (string, error)
}

type AuctioneerBBS interface {
	//services
	GetAllReps() ([]models.RepPresence, error)

	//lrp
	WatchForLRPStartAuction() (<-chan models.LRPStartAuction, chan<- bool, <-chan error)
	ClaimLRPStartAuction(models.LRPStartAuction) error
	ResolveLRPStartAuction(models.LRPStartAuction) error
}

type StagerBBS interface {
	//task
	WatchForCompletedTask() (<-chan models.Task, chan<- bool, <-chan error)
	DesireTask(models.Task) (models.Task, error)
	ResolvingTask(models.Task) (models.Task, error)
	ResolveTask(models.Task) (models.Task, error)

	//services
	GetAvailableFileServer() (string, error)
}

type MetricsBBS interface {
	//task
	GetAllTasks() ([]models.Task, error)

	//services
	GetServiceRegistrations() (models.ServiceRegistrations, error)
}

type FileServerBBS interface {
	//services
	MaintainFileServerPresence(
		heartbeatInterval time.Duration,
		fileServerURL string,
		fileServerId string,
	) (presence services_bbs.Presence, disappeared <-chan bool, err error)
}

type LRPRouterBBS interface {
	// lrp
	WatchForDesiredLongRunningProcesses() (<-chan models.DesiredLRP, chan<- bool, <-chan error)
	WatchForActualLongRunningProcesses() (<-chan models.LRP, chan<- bool, <-chan error)
	GetAllDesiredLongRunningProcesses() ([]models.DesiredLRP, error)
	GetAllActualLongRunningProcesses() ([]models.LRP, error)
}

func NewExecutorBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) ExecutorBBS {
	return NewBBS(store, timeProvider)
}

func NewRepBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) RepBBS {
	return NewBBS(store, timeProvider)
}

func NewConvergerBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) ConvergerBBS {
	return NewBBS(store, timeProvider)
}

func NewAppManagerBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) AppManagerBBS {
	return NewBBS(store, timeProvider)
}

func NewAuctioneerBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) AuctioneerBBS {
	return NewBBS(store, timeProvider)
}

func NewStagerBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) StagerBBS {
	return NewBBS(store, timeProvider)
}

func NewMetricsBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) MetricsBBS {
	return NewBBS(store, timeProvider)
}

func NewFileServerBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) FileServerBBS {
	return NewBBS(store, timeProvider)
}

func NewLRPRouterBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) LRPRouterBBS {
	return NewBBS(store, timeProvider)
}

func NewBBS(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) *BBS {
	return &BBS{
		LockBBS:               lock_bbs.New(store),
		LongRunningProcessBBS: lrp_bbs.New(store),
		ServicesBBS:           services_bbs.New(store),
		TaskBBS:               task_bbs.New(store, timeProvider),
	}
}

type BBS struct {
	*lock_bbs.LockBBS
	*lrp_bbs.LongRunningProcessBBS
	*services_bbs.ServicesBBS
	*task_bbs.TaskBBS
}
