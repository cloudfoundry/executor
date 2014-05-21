package fake_bbs

import (
	"sync"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeAuctioneerBBS struct {
	LRPStartAuctionChan      chan models.LRPStartAuction
	LRPStartAuctionStopChan  chan bool
	LRPStartAuctionErrorChan chan error

	claimedLRPStartAuctions   []models.LRPStartAuction
	claimLRPStartAuctionError error

	resolvedLRPStartAuction     models.LRPStartAuction
	resolveLRPStartAuctionError error

	reps []models.RepPresence

	lock *sync.Mutex
}

func NewFakeAuctioneerBBS() *FakeAuctioneerBBS {
	return &FakeAuctioneerBBS{
		LRPStartAuctionChan:      make(chan models.LRPStartAuction),
		LRPStartAuctionStopChan:  make(chan bool),
		LRPStartAuctionErrorChan: make(chan error),
		lock: &sync.Mutex{},
	}
}

func (bbs *FakeAuctioneerBBS) GetAllReps() ([]models.RepPresence, error) {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()
	return bbs.reps, nil
}

func (bbs *FakeAuctioneerBBS) SetAllReps(reps []models.RepPresence) {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	bbs.reps = reps
}

func (bbs *FakeAuctioneerBBS) WatchForLRPStartAuction() (<-chan models.LRPStartAuction, chan<- bool, <-chan error) {
	return bbs.LRPStartAuctionChan, bbs.LRPStartAuctionStopChan, bbs.LRPStartAuctionErrorChan
}

func (bbs *FakeAuctioneerBBS) ClaimLRPStartAuction(auction models.LRPStartAuction) error {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	bbs.claimedLRPStartAuctions = append(bbs.claimedLRPStartAuctions, auction)
	return bbs.claimLRPStartAuctionError
}

func (bbs *FakeAuctioneerBBS) GetClaimedLRPStartAuctions() []models.LRPStartAuction {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	return bbs.claimedLRPStartAuctions
}

func (bbs *FakeAuctioneerBBS) SetClaimLRPStartAuctionError(err error) {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	bbs.claimLRPStartAuctionError = err
}

func (bbs *FakeAuctioneerBBS) ResolveLRPStartAuction(auction models.LRPStartAuction) error {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	bbs.resolvedLRPStartAuction = auction
	return bbs.resolveLRPStartAuctionError
}

func (bbs *FakeAuctioneerBBS) GetResolvedLRPStartAuction() models.LRPStartAuction {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	return bbs.resolvedLRPStartAuction
}

func (bbs *FakeAuctioneerBBS) SetResolveLRPStartAuctionError(err error) {
	bbs.lock.Lock()
	defer bbs.lock.Unlock()

	bbs.resolveLRPStartAuctionError = err
}
