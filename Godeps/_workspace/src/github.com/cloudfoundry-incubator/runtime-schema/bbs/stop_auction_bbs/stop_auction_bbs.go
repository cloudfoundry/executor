package stop_auction_bbs

import (
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type StopAuctionBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
	logger       *gosteno.Logger
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider, logger *gosteno.Logger) *StopAuctionBBS {
	return &StopAuctionBBS{
		store:        store,
		timeProvider: timeProvider,
		logger:       logger,
	}
}
