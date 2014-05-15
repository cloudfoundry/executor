package task_bbs

import (
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type TaskBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider) *TaskBBS {
	return &TaskBBS{
		store:        store,
		timeProvider: timeProvider,
	}
}
