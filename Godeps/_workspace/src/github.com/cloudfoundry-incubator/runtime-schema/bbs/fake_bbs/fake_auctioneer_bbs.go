package fake_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type FakeAuctioneerBBS struct {
	*sync.Mutex

	LRPStartAuctionChan      chan models.LRPStartAuction
	LRPStartAuctionStopChan  chan bool
	LRPStartAuctionErrorChan chan error

	LockChannel        chan bool
	ReleaseLockChannel chan chan bool
	LockError          error

	ClaimedLRPStartAuctions   []models.LRPStartAuction
	ClaimLRPStartAuctionError error

	ResolvedLRPStartAuction     models.LRPStartAuction
	ResolveLRPStartAuctionError error

	Reps []models.RepPresence
}

func NewFakeAuctioneerBBS() *FakeAuctioneerBBS {
	return &FakeAuctioneerBBS{
		Mutex:                    &sync.Mutex{},
		LRPStartAuctionChan:      make(chan models.LRPStartAuction),
		LRPStartAuctionStopChan:  make(chan bool),
		LRPStartAuctionErrorChan: make(chan error),
		LockChannel:              make(chan bool),
		ReleaseLockChannel:       make(chan chan bool),
	}
}

func (bbs *FakeAuctioneerBBS) MaintainAuctioneerLock(interval time.Duration, auctioneerID string) (<-chan bool, chan<- chan bool, error) {
	return bbs.LockChannel, bbs.ReleaseLockChannel, bbs.LockError
}

func (bbs *FakeAuctioneerBBS) GetAllReps() ([]models.RepPresence, error) {
	bbs.Lock()
	defer bbs.Unlock()
	return bbs.Reps, nil
}

func (bbs *FakeAuctioneerBBS) WatchForLRPStartAuction() (<-chan models.LRPStartAuction, chan<- bool, <-chan error) {
	bbs.Lock()
	defer bbs.Unlock()

	return bbs.LRPStartAuctionChan, bbs.LRPStartAuctionStopChan, bbs.LRPStartAuctionErrorChan
}

func (bbs *FakeAuctioneerBBS) ClaimLRPStartAuction(auction models.LRPStartAuction) error {
	bbs.Lock()
	defer bbs.Unlock()

	bbs.ClaimedLRPStartAuctions = append(bbs.ClaimedLRPStartAuctions, auction)
	return bbs.ClaimLRPStartAuctionError
}

func (bbs *FakeAuctioneerBBS) ResolveLRPStartAuction(auction models.LRPStartAuction) error {
	bbs.Lock()
	defer bbs.Unlock()

	bbs.ResolvedLRPStartAuction = auction
	return bbs.ResolveLRPStartAuctionError
}

func (bbs *FakeAuctioneerBBS) GetClaimedLRPStartAuctions() []models.LRPStartAuction {
	bbs.Lock()
	defer bbs.Unlock()
	return bbs.ClaimedLRPStartAuctions
}

func (bbs *FakeAuctioneerBBS) GetResolvedLRPStartAuction() models.LRPStartAuction {
	bbs.Lock()
	defer bbs.Unlock()
	return bbs.ResolvedLRPStartAuction
}
