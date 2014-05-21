package lrp_bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/bbs/shared"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type LongRunningProcessBBS struct {
	store storeadapter.StoreAdapter
}

func New(store storeadapter.StoreAdapter) *LongRunningProcessBBS {
	return &LongRunningProcessBBS{
		store: store,
	}
}

func (bbs *LongRunningProcessBBS) ReportLongRunningProcessAsRunning(lrp models.LRP) error {
	return shared.RetryIndefinitelyOnStoreTimeout(func() error {
		return bbs.store.Create(storeadapter.StoreNode{
			Key:   shared.LRPSchemaPath(lrp),
			Value: lrp.ToJSON(),
		})
	})
}
