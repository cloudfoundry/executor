package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"time"
)

type stagerBBS struct {
	store storeadapter.StoreAdapter
}

func (s *stagerBBS) WatchForCompletedRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(s.store, "completed")
}

// The stager calls this when it wants to desire a payload
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (s *stagerBBS) DesireRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		if runOnce.CreatedAt == 0 {
			runOnce.CreatedAt = time.Now().UnixNano()
		}
		return s.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   runOnceSchemaPath("pending", runOnce.Guid),
				Value: runOnce.ToJSON(),
			},
		})
	})
}

func (s *stagerBBS) ResolvingRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Create(storeadapter.StoreNode{
			Key:   runOnceSchemaPath("resolving", runOnce.Guid),
			Value: runOnce.ToJSON(),
			TTL:   uint64(ResolvingTTL.Seconds()),
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (s *stagerBBS) ResolveRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(runOnceSchemaPath("pending", runOnce.Guid))
	})
}
