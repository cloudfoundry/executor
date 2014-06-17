package lrp_bbs_test

import (
	"path"
	"time"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LrpAuctionConvergence", func() {
	var pendingKickDuration time.Duration
	var claimedExpirationDuration time.Duration
	BeforeEach(func() {
		pendingKickDuration = 30 * time.Second
		claimedExpirationDuration = 5 * time.Minute
	})

	Context("when the LRPAuction has invalid JSON", func() {
		key := path.Join(shared.LRPStopAuctionSchemaRoot, "process-guid", "1")
		BeforeEach(func() {
			etcdClient.Create(storeadapter.StoreNode{
				Key:   key,
				Value: []byte("ß"),
			})
		})

		It("should be removed", func() {
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			_, err := etcdClient.Get(key)
			Ω(err).Should(MatchError(storeadapter.ErrorKeyNotFound))
		})
	})

	Describe("Kicking pending auctions", func() {
		var startAuctionEvents <-chan models.LRPStopAuction
		var auction models.LRPStopAuction

		commenceWatching := func() {
			startAuctionEvents, _, _ = bbs.WatchForLRPStopAuction()
		}

		BeforeEach(func() {
			auction = models.LRPStopAuction{
				Index:       1,
				ProcessGuid: "process-guid",
			}
			err := bbs.RequestLRPStopAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("Kicks auctions that haven't been updated in the given amount of time", func() {
			commenceWatching()

			timeProvider.Increment(pendingKickDuration)
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			Consistently(startAuctionEvents).ShouldNot(Receive())

			var noticedOnce models.LRPStopAuction
			timeProvider.Increment(time.Second)
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			Eventually(startAuctionEvents).Should(Receive(&noticedOnce))
			Ω(noticedOnce.Index).Should(Equal(auction.Index))
		})
	})

	Describe("Deleting very old claimed events", func() {
		BeforeEach(func() {
			auction := models.LRPStopAuction{
				Index:       1,
				ProcessGuid: "process-guid",
			}
			err := bbs.RequestLRPStopAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
			auction.State = models.LRPStopAuctionStatePending
			auction.UpdatedAt = timeProvider.Time().UnixNano()

			err = bbs.ClaimLRPStopAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should delete claimed events that have expired", func() {
			timeProvider.Increment(pendingKickDuration)
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStopAuctions()).Should(HaveLen(1))

			timeProvider.Increment(claimedExpirationDuration - pendingKickDuration)
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStopAuctions()).Should(HaveLen(1))

			timeProvider.Increment(time.Second)
			bbs.ConvergeLRPStopAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStopAuctions()).Should(BeEmpty())
		})
	})
})
