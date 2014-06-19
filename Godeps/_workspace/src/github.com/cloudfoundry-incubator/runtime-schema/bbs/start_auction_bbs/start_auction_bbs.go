package start_auction_bbs

import (
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/gunk/timeprovider"
	"github.com/cloudfoundry/storeadapter"
)

type StartAuctionBBS struct {
	store        storeadapter.StoreAdapter
	timeProvider timeprovider.TimeProvider
	logger       *gosteno.Logger
}

func New(store storeadapter.StoreAdapter, timeProvider timeprovider.TimeProvider, logger *gosteno.Logger) *StartAuctionBBS {
	return &StartAuctionBBS{
		store:        store,
		timeProvider: timeProvider,
		logger:       logger,
	}
}
