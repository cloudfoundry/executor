package lrp_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type compareAndSwappableLRPStartAuction struct {
	OldIndex           uint64
	NewLRPStartAuction models.LRPStartAuction
}

func (bbs *LRPBBS) ConvergeLRPStartAuctions(kickPendingDuration time.Duration, expireClaimedDuration time.Duration) {
	node, err := bbs.store.ListRecursively(shared.LRPStartAuctionSchemaRoot)
	if err != nil && err != storeadapter.ErrorKeyNotFound {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "start-auction-converge.failed-to-get-start-auctions")
		return
	}

	var keysToDelete []string
	var auctionsToCAS []compareAndSwappableLRPStartAuction

	for _, node := range node.ChildNodes {
		for _, node := range node.ChildNodes {
			auction, err := models.NewLRPStartAuctionFromJSON(node.Value)
			if err != nil {
				bbs.logger.Infod(map[string]interface{}{
					"error": err.Error(),
				}, "start-auction-converge.pruning-unparseable-start-auction-json")

				keysToDelete = append(keysToDelete, node.Key)
				continue
			}

			updatedAt := time.Unix(0, auction.UpdatedAt)
			switch auction.State {
			case models.LRPStartAuctionStatePending:
				if bbs.timeProvider.Time().Sub(updatedAt) > kickPendingDuration {
					bbs.logger.Infod(map[string]interface{}{
						"auction":       auction,
						"kick-duration": kickPendingDuration,
					}, "start-auction-converge.kick-pending-auction")

					auctionsToCAS = append(auctionsToCAS, compareAndSwappableLRPStartAuction{
						OldIndex:           node.Index,
						NewLRPStartAuction: auction,
					})
				}
			case models.LRPStartAuctionStateClaimed:
				if bbs.timeProvider.Time().Sub(updatedAt) > expireClaimedDuration {
					bbs.logger.Infod(map[string]interface{}{
						"auction":             auction,
						"expiration-duration": expireClaimedDuration,
					}, "start-auction-converge.removing-expired-claimed-auction")

					keysToDelete = append(keysToDelete, node.Key)
				}
			}
		}
	}

	bbs.store.Delete(keysToDelete...)
	bbs.batchCompareAndSwapLRPStartAuctions(auctionsToCAS)
}

func (bbs *LRPBBS) batchCompareAndSwapLRPStartAuctions(auctionsToCAS []compareAndSwappableLRPStartAuction) {
	waitGroup := &sync.WaitGroup{}
	waitGroup.Add(len(auctionsToCAS))
	for _, auctionToCAS := range auctionsToCAS {
		auction := auctionToCAS.NewLRPStartAuction
		newStoreNode := storeadapter.StoreNode{
			Key:   shared.LRPStartAuctionSchemaPath(auction),
			Value: auction.ToJSON(),
		}

		go func(auctionToCAS compareAndSwappableLRPStartAuction, newStoreNode storeadapter.StoreNode) {
			err := bbs.store.CompareAndSwapByIndex(auctionToCAS.OldIndex, newStoreNode)
			if err != nil {
				bbs.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "start-auction-converge.failed-to-compare-and-swap")
			}
			waitGroup.Done()
		}(auctionToCAS, newStoreNode)
	}

	waitGroup.Wait()
}
