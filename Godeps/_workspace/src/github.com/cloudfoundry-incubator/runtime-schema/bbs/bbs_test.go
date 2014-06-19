package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"

	. "github.com/onsi/ginkgo"
)

var _ = Describe("BBS", func() {
	It("should compile and be able to construct and return each BBS", func() {
		NewBBS(etcdClient, timeProvider, logger)
		NewExecutorBBS(etcdClient, timeProvider, logger)
		NewRepBBS(etcdClient, timeProvider, logger)
		NewConvergerBBS(etcdClient, timeProvider, logger)
		NewAppManagerBBS(etcdClient, timeProvider, logger)
		NewNsyncBBS(etcdClient, timeProvider, logger)
		NewAuctioneerBBS(etcdClient, timeProvider, logger)
		NewStagerBBS(etcdClient, timeProvider, logger)
		NewMetricsBBS(etcdClient, timeProvider, logger)
		NewFileServerBBS(etcdClient, timeProvider, logger)
		NewRouteEmitterBBS(etcdClient, timeProvider, logger)
		NewTPSBBS(etcdClient, timeProvider, logger)
	})
})
