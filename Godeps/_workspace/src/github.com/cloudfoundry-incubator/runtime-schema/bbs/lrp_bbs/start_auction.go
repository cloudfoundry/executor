package lrp_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LRPBBS) RequestLRPStartAuction(lrp models.LRPStartAuction) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		lrp.State = models.LRPStartAuctionStatePending
		lrp.UpdatedAt = bbs.timeProvider.Time().UnixNano()

		return bbs.store.Create(storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(lrp),
			Value: lrp.ToJSON(),
		})
	})
}

func (bbs *LRPBBS) ClaimLRPStartAuction(lrp models.LRPStartAuction) error {
	originalValue := lrp.ToJSON()

	lrp.State = models.LRPStartAuctionStateClaimed
	lrp.UpdatedAt = bbs.timeProvider.Time().UnixNano()
	changedValue := lrp.ToJSON()

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(lrp),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(lrp),
			Value: changedValue,
		})
	})
}

func (s *LRPBBS) ResolveLRPStartAuction(lrp models.LRPStartAuction) error {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(shared.LRPStartAuctionSchemaPath(lrp))
	})
	return err
}

func (bbs *LRPBBS) GetAllLRPStartAuctions() ([]models.LRPStartAuction, error) {
	lrps := []models.LRPStartAuction{}

	node, err := bbs.store.ListRecursively(shared.LRPStartAuctionSchemaRoot)
	if err == storeadapter.ErrorKeyNotFound {
		return lrps, nil
	}

	if err != nil {
		return lrps, err
	}

	for _, node := range node.ChildNodes {
		for _, node := range node.ChildNodes {
			lrp, err := models.NewLRPStartAuctionFromJSON(node.Value)
			if err != nil {
				return lrps, fmt.Errorf("cannot parse lrp JSON for key %s: %s", node.Key, err.Error())
			} else {
				lrps = append(lrps, lrp)
			}
		}
	}

	return lrps, nil
}

func (bbs *LRPBBS) WatchForLRPStartAuction() (<-chan models.LRPStartAuction, chan<- bool, <-chan error) {
	lrps := make(chan models.LRPStartAuction)

	filter := func(event storeadapter.WatchEvent) (models.LRPStartAuction, bool) {
		switch event.Type {
		case storeadapter.CreateEvent, storeadapter.UpdateEvent:
			lrp, err := models.NewLRPStartAuctionFromJSON(event.Node.Value)
			if err != nil {
				return models.LRPStartAuction{}, false
			}

			if lrp.State == models.LRPStartAuctionStatePending {
				return lrp, true
			}
		}
		return models.LRPStartAuction{}, false
	}

	stop, errs := shared.WatchWithFilter(bbs.store, shared.LRPStartAuctionSchemaRoot, lrps, filter)

	return lrps, stop, errs
}
