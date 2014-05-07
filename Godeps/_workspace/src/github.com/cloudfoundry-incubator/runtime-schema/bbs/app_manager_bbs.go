package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
)

type appManagerBBS struct {
	store storeadapter.StoreAdapter
}

func (bbs *appManagerBBS) DesireTransitionalLongRunningProcess(lrp models.TransitionalLongRunningProcess) error {
	return retryIndefinitelyOnStoreTimeout(func() error {
		lrp.State = models.TransitionalLRPStateDesired
		return bbs.store.SetMulti([]storeadapter.StoreNode{
			{
				Key:   transitionalLongRunningProcessSchemaPath(lrp),
				Value: lrp.ToJSON(),
			},
		})
	})
}
