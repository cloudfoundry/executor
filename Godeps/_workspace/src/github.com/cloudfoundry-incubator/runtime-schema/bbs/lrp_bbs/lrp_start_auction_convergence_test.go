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
		key := path.Join(shared.LRPStartAuctionSchemaRoot, "process-guid", "1")
		BeforeEach(func() {
			etcdClient.Create(storeadapter.StoreNode{
				Key:   key,
				Value: []byte("ß"),
			})
		})

		It("should be removed", func() {
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			_, err := etcdClient.Get(key)
			Ω(err).Should(MatchError(storeadapter.ErrorKeyNotFound))
		})
	})

	Describe("Kicking pending auctions", func() {
		var startAuctionEvents <-chan models.LRPStartAuction
		var auction models.LRPStartAuction

		commenceWatching := func() {
			startAuctionEvents, _, _ = bbs.WatchForLRPStartAuction()
		}

		BeforeEach(func() {
			auction = models.LRPStartAuction{
				Index:        1,
				ProcessGuid:  "process-guid",
				InstanceGuid: "instance-guid",
			}
			err := bbs.RequestLRPStartAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("Kicks auctions that haven't been updated in the given amount of time", func() {
			commenceWatching()

			timeProvider.Increment(pendingKickDuration)
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			Consistently(startAuctionEvents).ShouldNot(Receive())

			var noticedOnce models.LRPStartAuction
			timeProvider.Increment(time.Second)
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			Eventually(startAuctionEvents).Should(Receive(&noticedOnce))
			Ω(noticedOnce.Index).Should(Equal(auction.Index))
		})
	})

	Describe("Deleting very old claimed events", func() {
		BeforeEach(func() {
			auction := models.LRPStartAuction{
				Index:        1,
				ProcessGuid:  "process-guid",
				InstanceGuid: "instance-guid",
			}
			err := bbs.RequestLRPStartAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
			auction.State = models.LRPStartAuctionStatePending
			auction.UpdatedAt = timeProvider.Time().UnixNano()

			err = bbs.ClaimLRPStartAuction(auction)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should delete claimed events that have expired", func() {
			timeProvider.Increment(pendingKickDuration)
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStartAuctions()).Should(HaveLen(1))

			timeProvider.Increment(claimedExpirationDuration - pendingKickDuration)
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStartAuctions()).Should(HaveLen(1))

			timeProvider.Increment(time.Second)
			bbs.ConvergeLRPStartAuctions(pendingKickDuration, claimedExpirationDuration)
			Ω(bbs.GetAllLRPStartAuctions()).Should(BeEmpty())
		})
	})
})
