package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type stagerBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
}

func (s *stagerBBS) WatchForCompletedTask() (<-chan *models.Task, chan<- bool, <-chan error) {
	return watchForTaskModificationsOnState(s.store, models.TaskStateCompleted)
}

// The stager calls this when it wants to desire a payload
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (s *stagerBBS) DesireTask(task *models.Task) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		if task.CreatedAt == 0 {
			task.CreatedAt = s.timeProvider.Time().UnixNano()
		}
		task.UpdatedAt = s.timeProvider.Time().UnixNano()
		task.State = models.TaskStatePending
		return s.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   taskSchemaPath(task),
				Value: task.ToJSON(),
			},
		})
	})
}

func (s *stagerBBS) ResolvingTask(task *models.Task) error {
	originalValue := task.ToJSON()

	task.UpdatedAt = s.timeProvider.Time().UnixNano()
	task.State = models.TaskStateResolving

	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   taskSchemaPath(task),
			Value: task.ToJSON(),
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (s *stagerBBS) ResolveTask(task *models.Task) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(taskSchemaPath(task))
	})
}
