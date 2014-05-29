package fake_bbs_test

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs"
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs/fake_bbs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FakeBbs", func() {
	It("should provide fakes that satisfy the interfaces", func() {
		var executorBBS bbs.ExecutorBBS
		executorBBS = NewFakeExecutorBBS()
		Ω(executorBBS).ShouldNot(BeNil())

		var repBBS bbs.RepBBS
		repBBS = NewFakeRepBBS()
		Ω(repBBS).ShouldNot(BeNil())

		var convergerBBS bbs.ConvergerBBS
		convergerBBS = NewFakeConvergerBBS()
		Ω(convergerBBS).ShouldNot(BeNil())

		var appManagerBBS bbs.AppManagerBBS
		appManagerBBS = NewFakeAppManagerBBS()
		Ω(appManagerBBS).ShouldNot(BeNil())

		var auctioneerBBS bbs.AuctioneerBBS
		auctioneerBBS = NewFakeAuctioneerBBS()
		Ω(auctioneerBBS).ShouldNot(BeNil())

		var stagerBBS bbs.StagerBBS
		stagerBBS = NewFakeStagerBBS()
		Ω(stagerBBS).ShouldNot(BeNil())

		var metricsBBS bbs.MetricsBBS
		metricsBBS = NewFakeMetricsBBS()
		Ω(metricsBBS).ShouldNot(BeNil())

		var lrpRouterBBS bbs.LRPRouterBBS
		lrpRouterBBS = NewFakeLRPRouterBBS()
		Ω(lrpRouterBBS).ShouldNot(BeNil())
	})
})
