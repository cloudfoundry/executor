package task_bbs

import (
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type TaskBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
	logger       *steno.Logger
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider, logger *steno.Logger) *TaskBBS {
	return &TaskBBS{
		store:        store,
		timeProvider: timeProvider,
		logger:       logger,
	}
}
