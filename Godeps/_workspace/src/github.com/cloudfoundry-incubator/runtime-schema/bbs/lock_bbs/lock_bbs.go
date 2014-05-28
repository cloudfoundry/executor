package lock_bbs

import (
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry/storeadapter"
)

type LockBBS struct {
	store storeadapter.StoreAdapter
}

func New(store storeadapter.StoreAdapter) *LockBBS {
	return &LockBBS{
		store: store,
	}
}

func (self *LockBBS) MaintainAuctioneerLock(interval time.Duration, auctioneerID string) (<-chan bool, chan<- chan bool, error) {
	return self.store.MaintainNode(storeadapter.StoreNode{
		Key:   shared.LockSchemaPath("auctioneer_lock"),
		Value: []byte(auctioneerID),
		TTL:   uint64(interval.Seconds()),
	})
}

func (self *LockBBS) MaintainConvergeLock(interval time.Duration, convergerID string) (<-chan bool, chan<- chan bool, error) {
	return self.store.MaintainNode(storeadapter.StoreNode{
		Key:   shared.LockSchemaPath("converge_lock"),
		Value: []byte(convergerID),
		TTL:   uint64(interval.Seconds()),
	})
}
