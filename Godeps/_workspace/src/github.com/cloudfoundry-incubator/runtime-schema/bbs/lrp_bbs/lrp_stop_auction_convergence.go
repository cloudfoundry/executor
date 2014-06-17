package lrp_bbs

import (
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type compareAndSwappableLRPStopAuction struct {
	OldIndex          uint64
	NewLRPStopAuction models.LRPStopAuction
}

func (bbs *LRPBBS) ConvergeLRPStopAuctions(kickPendingDuration time.Duration, expireClaimedDuration time.Duration) {
	node, err := bbs.store.ListRecursively(shared.LRPStopAuctionSchemaRoot)
	if err != nil && err != storeadapter.ErrorKeyNotFound {
		bbs.logger.Errord(map[string]interface{}{
			"error": err.Error(),
		}, "stop-auction-converge.failed-to-get-stop-auctions")
		return
	}

	var keysToDelete []string
	var auctionsToCAS []compareAndSwappableLRPStopAuction

	for _, node := range node.ChildNodes {
		for _, node := range node.ChildNodes {
			auction, err := models.NewLRPStopAuctionFromJSON(node.Value)
			if err != nil {
				bbs.logger.Infod(map[string]interface{}{
					"error": err.Error(),
				}, "stop-auction-converge.pruning-unparseable-stop-auction-json")

				keysToDelete = append(keysToDelete, node.Key)
				continue
			}

			updatedAt := time.Unix(0, auction.UpdatedAt)
			switch auction.State {
			case models.LRPStopAuctionStatePending:
				if bbs.timeProvider.Time().Sub(updatedAt) > kickPendingDuration {
					bbs.logger.Infod(map[string]interface{}{
						"auction":       auction,
						"kick-duration": kickPendingDuration,
					}, "stop-auction-converge.kick-pending-auction")

					auctionsToCAS = append(auctionsToCAS, compareAndSwappableLRPStopAuction{
						OldIndex:          node.Index,
						NewLRPStopAuction: auction,
					})
				}
			case models.LRPStopAuctionStateClaimed:
				if bbs.timeProvider.Time().Sub(updatedAt) > expireClaimedDuration {
					bbs.logger.Infod(map[string]interface{}{
						"auction":             auction,
						"expiration-duration": expireClaimedDuration,
					}, "stop-auction-converge.removing-expired-claimed-auction")

					keysToDelete = append(keysToDelete, node.Key)
				}
			}
		}
	}

	bbs.store.Delete(keysToDelete...)
	bbs.batchCompareAndSwapLRPStopAuctions(auctionsToCAS)
}

func (bbs *LRPBBS) batchCompareAndSwapLRPStopAuctions(auctionsToCAS []compareAndSwappableLRPStopAuction) {
	waitGroup := &sync.WaitGroup{}
	waitGroup.Add(len(auctionsToCAS))
	for _, auctionToCAS := range auctionsToCAS {
		auction := auctionToCAS.NewLRPStopAuction
		newStoreNode := storeadapter.StoreNode{
			Key:   shared.LRPStopAuctionSchemaPath(auction),
			Value: auction.ToJSON(),
		}

		go func(auctionToCAS compareAndSwappableLRPStopAuction, newStoreNode storeadapter.StoreNode) {
			err := bbs.store.CompareAndSwapByIndex(auctionToCAS.OldIndex, newStoreNode)
			if err != nil {
				bbs.logger.Errord(map[string]interface{}{
					"error": err.Error(),
				}, "stop-auction-converge.failed-to-compare-and-swap")
			}
			waitGroup.Done()
		}(auctionToCAS, newStoreNode)
	}

	waitGroup.Wait()
}
