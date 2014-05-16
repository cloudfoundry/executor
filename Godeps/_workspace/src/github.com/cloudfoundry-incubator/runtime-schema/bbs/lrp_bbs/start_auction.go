package lrp_bbs

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

func (bbs *LongRunningProcessBBS) RequestLRPStartAuction(lrp models.LRPStartAuction) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		lrp.State = models.LRPStartAuctionStatePending
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   shared.LRPStartAuctionSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}

func (bbs *LongRunningProcessBBS) GetAllLRPStartAuctions() ([]models.LRPStartAuction, error) {
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

func (self *LongRunningProcessBBS) WatchForLRPStartAuction() (<-chan models.LRPStartAuction, chan<- bool, <-chan error) {
	return watchForAuctionLrpModificationsOnState(self.store, models.LRPStartAuctionStatePending)
}

func (self *LongRunningProcessBBS) ClaimLRPStartAuction(lrp models.LRPStartAuction) error {
	originalValue := lrp.ToJSON()

	lrp.State = models.LRPStartAuctionStateClaimed
	changedValue := lrp.ToJSON()

	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return self.store.CompareAndSwap(storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(lrp),
			Value: originalValue,
		}, storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(lrp),
			Value: changedValue,
		})
	})
}

func (s *LongRunningProcessBBS) ResolveLRPStartAuction(lrp models.LRPStartAuction) error {
	err := shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return s.store.Delete(shared.LRPStartAuctionSchemaPath(lrp))
	})
	return err
}

func watchForAuctionLrpModificationsOnState(store storeadapter.StoreAdapter, state models.LRPStartAuctionState) (<-chan models.LRPStartAuction, chan<- bool, <-chan error) {
	lrps := make(chan models.LRPStartAuction)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(shared.LRPStartAuctionSchemaRoot)

	go func() {
		defer close(lrps)
		defer close(errsOuter)

		for {
			select {
			case <-stopOuter:
				close(stopInner)
				return

			case event, ok := <-events:
				if !ok {
					return
				}
				lrp, err := models.NewLRPStartAuctionFromJSON(event.Node.Value)
				if err != nil {
					continue
				}

				if lrp.State == state {
					lrps <- lrp
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return lrps, stopOuter, errsOuter
}
