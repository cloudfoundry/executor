package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"time"
)

type stagerBBS struct {
	store storeadapter.StoreAdapter
}

func (self *stagerBBS) WatchForCompletedRunOnce() (<-chan models.RunOnce, chan<- bool, <-chan error) {
	return watchForRunOnceModificationsOnState(self.store, "completed")
}

// The stager calls this when it wants to desire a payload
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should bail and run its "this-failed-to-stage" routine
func (self *stagerBBS) DesireRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		if runOnce.CreatedAt == 0 {
			runOnce.CreatedAt = time.Now().UnixNano()
		}
		return self.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   runOnceSchemaPath("pending", runOnce.Guid),
				Value: runOnce.ToJSON(),
			},
		})
	})
}

// The stager calls this when it wants to signal that it has received a completion and is handling it
// stagerBBS will retry this repeatedly if it gets a StoreTimeout error (up to N seconds?)
// If this fails, the stager should assume that someone else is handling the completion and should bail
func (self *stagerBBS) ResolveRunOnce(runOnce models.RunOnce) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		return self.store.Delete(runOnceSchemaPath("pending", runOnce.Guid))
	})
}
