package lrp_bbs_test

import (
	"time"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
)

var _ = Describe("Stop Auction", func() {
	Describe("RequestLRPStopAuction", func() {
		var auctionLRP models.LRPStopAuction

		BeforeEach(func() {
			auctionLRP = models.LRPStopAuction{
				ProcessGuid: "some-guid",
				Index:       1,
			}
		})

		It("creates /v1/stop/<guid>/index", func() {
			err := bbs.RequestLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/stop/some-guid/1")
			Ω(err).ShouldNot(HaveOccurred())

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Ω(node.Value).Should(Equal(auctionLRP.ToJSON()))
		})

		Context("when the key already exists", func() {
			It("should error", func() {
				err := bbs.RequestLRPStopAuction(auctionLRP)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.RequestLRPStopAuction(auctionLRP)
				Ω(err).Should(MatchError(storeadapter.ErrorKeyExists))
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.RequestLRPStopAuction(auctionLRP)
			})
		})
	})

	Describe("WatchForLRPStopAuction", func() {
		var (
			events     <-chan models.LRPStopAuction
			stop       chan<- bool
			errors     <-chan error
			auctionLRP models.LRPStopAuction
		)

		BeforeEach(func() {
			auctionLRP = models.LRPStopAuction{
				ProcessGuid: "some-guid",
				Index:       1,
			}
			events, stop, errors = bbs.WatchForLRPStopAuction()
		})

		AfterEach(func() {
			stop <- true
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.RequestLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Eventually(events).Should(Receive(Equal(auctionLRP)))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.RequestLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Eventually(events).Should(Receive(Equal(auctionLRP)))

			err = etcdClient.SetMulti([]storeadapter.StoreNode{
				{
					Key:   shared.LRPStopAuctionSchemaPath(auctionLRP),
					Value: auctionLRP.ToJSON(),
				},
			})
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(auctionLRP)))
		})

		It("does not send an event down the pipe for deletes", func() {
			err := bbs.RequestLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Eventually(events).Should(Receive(Equal(auctionLRP)))

			err = bbs.ResolveLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			Consistently(events).ShouldNot(Receive())
		})
	})

	Describe("ClaimLRPStopAuction", func() {
		var auctionLRP models.LRPStopAuction

		BeforeEach(func() {
			auctionLRP = models.LRPStopAuction{
				ProcessGuid: "some-guid",
				Index:       1,
			}

			err := bbs.RequestLRPStopAuction(auctionLRP)

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when claiming a requested LRP auction", func() {
			It("sets the state to claimed", func() {
				timeProvider.Increment(time.Minute)

				err := bbs.ClaimLRPStopAuction(auctionLRP)
				Ω(err).ShouldNot(HaveOccurred())

				expectedAuctionLRP := auctionLRP
				expectedAuctionLRP.State = models.LRPStopAuctionStateClaimed
				expectedAuctionLRP.UpdatedAt = timeProvider.Time().UnixNano()

				node, err := etcdClient.Get("/v1/stop/some-guid/1")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
					Key:   "/v1/stop/some-guid/1",
					Value: expectedAuctionLRP.ToJSON(),
				}))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.ClaimLRPStopAuction(auctionLRP)
				})
			})
		})

		Context("When claiming an LRP auction that is not in the pending state", func() {
			BeforeEach(func() {
				err := bbs.ClaimLRPStopAuction(auctionLRP)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns an error", func() {
				err := bbs.ClaimLRPStopAuction(auctionLRP)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Describe("ResolveLRPStopAuction", func() {
		var auctionLRP models.LRPStopAuction

		BeforeEach(func() {
			auctionLRP = models.LRPStopAuction{
				ProcessGuid: "some-guid",
				Index:       1,
			}

			err := bbs.RequestLRPStopAuction(auctionLRP)

			auctionLRP.State = models.LRPStopAuctionStatePending
			auctionLRP.UpdatedAt = timeProvider.Time().UnixNano()
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should remove /v1/stop/<guid>/<index>", func() {
			err := bbs.ResolveLRPStopAuction(auctionLRP)
			Ω(err).ShouldNot(HaveOccurred())

			_, err = etcdClient.Get("/v1/stop/some-guid/1")
			Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				err := bbs.ResolveLRPStopAuction(auctionLRP)
				return err
			})
		})
	})
})
