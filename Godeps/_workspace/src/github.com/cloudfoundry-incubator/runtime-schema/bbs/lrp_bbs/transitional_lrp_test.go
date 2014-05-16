package lrp_bbs_test

import (
	"time"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/lrp_bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/cloudfoundry/storeadapter/storenodematchers"
)

var _ = Describe("LongRunningProcess BBS", func() {
	var bbs *LongRunningProcessBBS
	var lrp models.TransitionalLongRunningProcess

	BeforeEach(func() {
		bbs = New(etcdClient)

		lrp = models.TransitionalLongRunningProcess{
			Guid:  "some-guid",
			State: models.TransitionalLRPStateInvalid,
			Actions: []models.ExecutorAction{
				{
					Action: models.RunAction{
						Script: "cat /tmp/file",
						Env: []models.EnvironmentVariable{
							{
								Key:   "PATH",
								Value: "the-path",
							},
						},
						Timeout: time.Second,
					},
				},
			},
		}
	})

	Describe("DesireTransitionalLongRunningProcess", func() {
		It("creates /transitional_lrp/<guid>", func() {
			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := etcdClient.Get("/v1/transitional_lrp/some-guid")
			Ω(err).ShouldNot(HaveOccurred())

			lrp.State = models.TransitionalLRPStateDesired
			Ω(node.Value).Should(Equal(lrp.ToJSON()))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack(func() error {
				return bbs.DesireTransitionalLongRunningProcess(lrp)
			})
		})
	})

	Describe("WatchForDesiredTransitionalLongRunningProcesses", func() {
		var (
			events  <-chan models.TransitionalLongRunningProcess
			stop    chan<- bool
			errors  <-chan error
			stopped bool
		)

		BeforeEach(func() {
			events, stop, errors = bbs.WatchForDesiredTransitionalLongRunningProcess()
		})

		AfterEach(func() {
			if !stopped {
				stop <- true
			}
		})

		It("sends an event down the pipe for creates", func() {
			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			lrp.State = models.TransitionalLRPStateDesired
			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("sends an event down the pipe for updates", func() {
			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			lrp.State = models.TransitionalLRPStateDesired
			Eventually(events).Should(Receive(Equal(lrp)))

			err = bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(events).Should(Receive(Equal(lrp)))
		})

		It("closes the events and errors channel when told to stop", func() {
			stop <- true
			stopped = true

			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(events).Should(BeClosed())
			Ω(errors).Should(BeClosed())
		})
	})

	Describe("StartTransitionalLongRunningProcess", func() {
		BeforeEach(func() {
			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			lrp.State = models.TransitionalLRPStateDesired
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("when starting a desired LRP", func() {
			It("sets the state to running", func() {
				err := bbs.StartTransitionalLongRunningProcess(lrp)
				Ω(err).ShouldNot(HaveOccurred())

				expectedLrp := lrp
				expectedLrp.State = models.TransitionalLRPStateRunning

				node, err := etcdClient.Get("/v1/transitional_lrp/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(MatchStoreNode(storeadapter.StoreNode{
					Key:   "/v1/transitional_lrp/some-guid",
					Value: expectedLrp.ToJSON(),
				}))
			})

			Context("when the store is out of commission", func() {
				itRetriesUntilStoreComesBack(func() error {
					return bbs.StartTransitionalLongRunningProcess(lrp)
				})
			})
		})

		Context("When starting an LRP that is not in the desired state", func() {
			BeforeEach(func() {
				err := bbs.StartTransitionalLongRunningProcess(lrp)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns an error", func() {
				err := bbs.StartTransitionalLongRunningProcess(lrp)
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
