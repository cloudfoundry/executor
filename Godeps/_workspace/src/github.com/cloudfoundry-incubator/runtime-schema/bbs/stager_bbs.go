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

func (s *stagerBBS) WatchForCompletedRunOnce() (<-chan *models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(s.store, models.RunOnceStateCompleted)
}

// The stager calls this when it wants to desire a payload
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (s *stagerBBS) DesireRunOnce(runOnce *models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		if runOnce.CreatedAt == 0 {
			runOnce.CreatedAt = s.timeProvider.Time().UnixNano()
		}
		runOnce.UpdatedAt = s.timeProvider.Time().UnixNano()
		runOnce.State = models.RunOnceStatePending
		return s.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   runOnceSchemaPath(runOnce),
				Value: runOnce.ToJSON(),
			},
		})
	})
}

func (s *stagerBBS) ResolvingRunOnce(runOnce *models.RunOnce) error {
	originalValue := runOnce.ToJSON()

	runOnce.UpdatedAt = s.timeProvider.Time().UnixNano()
	runOnce.State = models.RunOnceStateResolving

	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   runOnceSchemaPath(runOnce),
			Value: runOnce.ToJSON(),
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (s *stagerBBS) ResolveRunOnce(runOnce *models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(runOnceSchemaPath(runOnce))
	})
}
