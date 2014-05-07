package bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/gunk/timeprovider/faketimeprovider"
)

var _ = Describe("App Manager BBS", func() {
	var bbs *BBS
	var timeProvider *faketimeprovider.FakeTimeProvider

	BeforeEach(func() {
		timeProvider = faketimeprovider.New(time.Unix(1238, 0))
		bbs = New(store, timeProvider)
	})

	Describe("DesireTransitionalLongRunningProcess", func() {
		var lrp models.TransitionalLongRunningProcess

		BeforeEach(func() {
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

		It("creates /transitional_lrp/<guid>", func() {
			err := bbs.DesireTransitionalLongRunningProcess(lrp)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := store.Get("/v1/transitional_lrp/some-guid")
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
})
