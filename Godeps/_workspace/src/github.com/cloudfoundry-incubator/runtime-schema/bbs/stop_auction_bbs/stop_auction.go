package stop_auction_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *StopAuctionBBS) RequestLRPStopAuction(lrp models.LRPStopAuction) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		lrp.State = models.LRPStopAuctionStatePending
		lrp.UpdatedAt = bbs.timeProvider.Time().UnixNano()

		return bbs.store.Create(storeadapter.StoreNode{
			Key:   shared.LRPStopAuctionSchemaPath(lrp),
			Value: lrp.ToJSON(),
		})
	})
}

func (bbs *StopAuctionBBS) ClaimLRPStopAuction(lrp models.LRPStopAuction) error {
	originalValue := lrp.ToJSON()

	lrp.State = models.LRPStopAuctionStateClaimed
	lrp.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	changedValue := lrp.ToJSON()

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.LRPStopAuctionSchemaPath(lrp),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.LRPStopAuctionSchemaPath(lrp),
			Value: changedValue,
		})
	})
}

func (s *StopAuctionBBS) ResolveLRPStopAuction(lrp models.LRPStopAuction) error {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(shared.LRPStopAuctionSchemaPath(lrp))
	})
	return err
}

func (bbs *StopAuctionBBS) GetAllLRPStopAuctions() ([]models.LRPStopAuction, error) {
	lrps := []models.LRPStopAuction{}

	node, err := bbs.store.ListRecursively(shared.LRPStopAuctionSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		for _, node := range node.ChildNodes {
			lrp, err := models.NewLRPStopAuctionFromJSON(node.Value)
			if err != nil {
				return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
			} else {
				lrps = append(lrps, lrp)
			}
		}
	}

	return lrps, nil
}

func (bbs *StopAuctionBBS) WatchForLRPStopAuction() (<-chan models.LRPStopAuction, chan<- bool, <-chan error) {
	lrps := make(chan models.LRPStopAuction)

	filter := func(event storeadapter.WatchEvent) (models.LRPStopAuction, bool) {
		switch event.Type {
		case storeadapter.CreateEvent, storeadapter.UpdateEvent:
			lrp, err := models.NewLRPStopAuctionFromJSON(event.Node.Value)
			if err != nil {
				return models.LRPStopAuction{}, false
			}

			if lrp.State == models.LRPStopAuctionStatePending {
				return lrp, true
			}
		}
		return models.LRPStopAuction{}, false
	}

	stop, errs := shared.WatchWithFilter(bbs.store, shared.LRPStopAuctionSchemaRoot, lrps, filter)

	return lrps, stop, errs
}
